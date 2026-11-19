package client

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/h0n9/msg-lake/proto"
	"github.com/postie-labs/go-postie-lib/crypto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type reconnectServer struct {
	pb.UnimplementedMsgLakeServer
	subscribe  func(pb.MsgLake_SubscribeServer) error
	publishes  atomic.Int32
	publishErr error
}

func (s *reconnectServer) Subscribe(_ *pb.SubscribeReq, stream pb.MsgLake_SubscribeServer) error {
	return s.subscribe(stream)
}
func (s *reconnectServer) Publish(context.Context, *pb.PublishReq) (*pb.PublishRes, error) {
	s.publishes.Add(1)
	if s.publishErr != nil {
		return nil, s.publishErr
	}
	return &pb.PublishRes{Ok: true}, nil
}

func newReconnectClient(t *testing.T, server pb.MsgLakeServer) *Client {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	pb.RegisterMsgLakeServer(grpcServer, server)
	go grpcServer.Serve(listener)
	t.Cleanup(grpcServer.Stop)
	conn, err := grpc.NewClient("passthrough:///test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenPrivKey()
	if err != nil {
		t.Fatal(err)
	}
	closed, cancel := context.WithCancel(context.Background())
	client := &Client{privKey: key, grpcClientConn: conn, msgLakeClient: pb.NewMsgLakeClient(conn), closed: closed, cancel: cancel, ackTimeout: 150 * time.Millisecond}
	t.Cleanup(client.Close)
	return client
}

func ack(stream pb.MsgLake_SubscribeServer, ok bool) error {
	return stream.Send(&pb.SubscribeRes{Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_ACK, Res: &pb.SubscribeRes_Ok{Ok: ok}})
}

func TestSubscribeReconnect(t *testing.T) {
	for _, first := range []string{"EOF before ACK", "EOF after ACK", "Unavailable", "ACK timeout"} {
		t.Run(first, func(t *testing.T) {
			var calls atomic.Int32
			server := &reconnectServer{subscribe: func(stream pb.MsgLake_SubscribeServer) error {
				if calls.Add(1) == 1 {
					switch first {
					case "EOF after ACK":
						if err := ack(stream, true); err != nil {
							return err
						}
					case "Unavailable":
						return status.Error(codes.Unavailable, "closing")
					case "ACK timeout":
						<-stream.Context().Done()
						return stream.Context().Err()
					}
					return nil
				}
				if err := ack(stream, true); err != nil {
					return err
				}
				return stream.Send(&pb.SubscribeRes{Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY, Res: &pb.SubscribeRes_TimestampedSignedMsgCapsule{TimestampedSignedMsgCapsule: &pb.TimestampedSignedMsgCapsule{Timestamp: 1}}})
			}}
			client := newReconnectClient(t, server)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var received int
			if err := client.Subscribe(ctx, "topic", func(*pb.TimestampedSignedMsgCapsule) error { received++; cancel(); return nil }); err != nil {
				t.Fatal(err)
			}
			if received != 1 || calls.Load() != 2 {
				t.Fatalf("received %d via %d calls", received, calls.Load())
			}
			if err := client.Publish(context.Background(), "topic", "message"); err != nil {
				t.Fatal(err)
			}
			if server.publishes.Load() != 1 {
				t.Fatal("publish failed on shared connection")
			}
		})
	}
}

func TestACKTimerDoesNotEndIdleStream(t *testing.T) {
	var calls atomic.Int32
	server := &reconnectServer{subscribe: func(stream pb.MsgLake_SubscribeServer) error {
		calls.Add(1)
		if err := ack(stream, true); err != nil {
			return err
		}
		time.Sleep(350 * time.Millisecond)
		return stream.Send(&pb.SubscribeRes{Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY, Res: &pb.SubscribeRes_TimestampedSignedMsgCapsule{TimestampedSignedMsgCapsule: &pb.TimestampedSignedMsgCapsule{Timestamp: 2}}})
	}}
	client := newReconnectClient(t, server)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var received int
	if err := client.Subscribe(ctx, "topic", func(*pb.TimestampedSignedMsgCapsule) error { received++; cancel(); return nil }); err != nil {
		t.Fatal(err)
	}
	if received != 1 || calls.Load() != 1 {
		t.Fatalf("idle stream was interrupted: deliveries=%d calls=%d", received, calls.Load())
	}
}

func TestSubscribeFailedACKAndClose(t *testing.T) {
	server := &reconnectServer{subscribe: func(stream pb.MsgLake_SubscribeServer) error { return ack(stream, false) }}
	client := newReconnectClient(t, server)
	if err := client.Subscribe(context.Background(), "topic", func(*pb.TimestampedSignedMsgCapsule) error { return nil }); err == nil {
		t.Fatal("failed ACK was retried")
	}
	client.Close()
	client.Close()
	done := make(chan error, 1)
	go func() {
		done <- client.Subscribe(context.Background(), "topic", func(*pb.TimestampedSignedMsgCapsule) error { return nil })
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe retried after Close")
	}
}

func TestSubscribeInvalidCapsuleDoesNotRetry(t *testing.T) {
	var calls atomic.Int32
	server := &reconnectServer{subscribe: func(stream pb.MsgLake_SubscribeServer) error {
		calls.Add(1)
		if err := ack(stream, true); err != nil {
			return err
		}
		return stream.Send(&pb.SubscribeRes{Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY, Res: &pb.SubscribeRes_TimestampedSignedMsgCapsule{TimestampedSignedMsgCapsule: &pb.TimestampedSignedMsgCapsule{}}})
	}}
	client := newReconnectClient(t, server)
	client.verifyReceivedMessages = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Subscribe(ctx, "topic", func(*pb.TimestampedSignedMsgCapsule) error { t.Fatal("invalid capsule reached handler"); return nil }); err == nil {
		t.Fatal("invalid capsule accepted")
	}
	if calls.Load() != 1 {
		t.Fatal("verification failure was retried")
	}
}

func TestSubscribeCloseDuringBackoff(t *testing.T) {
	entered := make(chan struct{}, 1)
	server := &reconnectServer{subscribe: func(stream pb.MsgLake_SubscribeServer) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		return status.Error(codes.Unavailable, "down")
	}}
	client := newReconnectClient(t, server)
	done := make(chan error, 1)
	go func() {
		done <- client.Subscribe(context.Background(), "topic", func(*pb.TimestampedSignedMsgCapsule) error { return nil })
	}()
	<-entered
	client.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry continued after Close")
	}
}

func TestSubscriptionRetryClassification(t *testing.T) {
	if !retrySubscription(io.EOF) || !retrySubscription(errACKTimeout) || !retrySubscription(status.Error(codes.Unavailable, "down")) || retrySubscription(status.Error(codes.ResourceExhausted, "slow")) || retrySubscription(errors.New("bad ACK")) {
		t.Fatal("unexpected retry classification")
	}
}

func TestPublishFailureIsNotReplayed(t *testing.T) {
	server := &reconnectServer{publishErr: status.Error(codes.Unavailable, "response lost")}
	client := newReconnectClient(t, server)
	if err := client.Publish(context.Background(), "topic", "message"); status.Code(err) != codes.Unavailable {
		t.Fatalf("got %v", err)
	}
	if server.publishes.Load() != 1 {
		t.Fatalf("publish called %d times", server.publishes.Load())
	}
}

func TestACKTimeoutCoversConnectionAttempt(t *testing.T) {
	conn, err := grpc.NewClient("passthrough:///missing", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { <-ctx.Done(); return nil, ctx.Err() }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := &Client{msgLakeClient: pb.NewMsgLakeClient(conn), ackTimeout: 100 * time.Millisecond}
	started := time.Now()
	_, err = client.subscribeAttempt(context.Background(), &pb.SubscribeReq{TopicId: "topic"}, "topic", func(*pb.TimestampedSignedMsgCapsule) error { return nil })
	if !errors.Is(err, errACKTimeout) {
		t.Fatalf("got %v, want ACK timeout", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("connection attempt exceeded ACK timeout")
	}
}

func TestConcurrentSubscriptionsPublishAndClose(t *testing.T) {
	started := make(chan struct{}, 2)
	server := &reconnectServer{subscribe: func(stream pb.MsgLake_SubscribeServer) error {
		if err := ack(stream, true); err != nil {
			return err
		}
		started <- struct{}{}
		<-stream.Context().Done()
		return stream.Context().Err()
	}}
	client := newReconnectClient(t, server)
	done := make(chan error, 2)
	for range 2 {
		go func() {
			done <- client.Subscribe(context.Background(), "topic", func(*pb.TimestampedSignedMsgCapsule) error { return nil })
		}()
	}
	<-started
	<-started
	if err := client.Publish(context.Background(), "topic", "payload"); err != nil {
		t.Fatal(err)
	}
	client.Close()
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("Subscribe did not stop")
		}
	}
}
