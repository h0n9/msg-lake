package lake

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/h0n9/msg-lake/msg"
	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/protocol"
	"github.com/postie-labs/go-postie-lib/crypto"
	"github.com/rs/zerolog"
)

func TestSubscriberStreamError(t *testing.T) {
	t.Run("slow subscriber", func(t *testing.T) {
		err := subscriberStreamError(msg.ErrSlowSubscriber)
		if got := status.Code(err); got != codes.ResourceExhausted {
			t.Fatalf("status code = %v, want %v", got, codes.ResourceExhausted)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		if err := subscriberStreamError(context.Canceled); err != nil {
			t.Fatalf("subscriberStreamError(context.Canceled) = %v, want nil", err)
		}
	})

	t.Run("other error", func(t *testing.T) {
		want := errors.New("send failed")
		if got := subscriberStreamError(want); !errors.Is(got, want) {
			t.Fatalf("subscriberStreamError() = %v, want %v", got, want)
		}
	})
}

type fakeSubscribeStream struct {
	ctx          context.Context
	sendEntered  chan struct{}
	sendRelease  chan struct{}
	relayEntered chan struct{}
	relayRelease chan struct{}
	sends        atomic.Int32
}

func (s *fakeSubscribeStream) Send(response *pb.SubscribeRes) error {
	s.sends.Add(1)
	if response.GetType() == pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY && s.relayRelease != nil {
		if s.relayEntered != nil {
			s.relayEntered <- struct{}{}
		}
		<-s.relayRelease
	}
	if s.sendEntered != nil {
		select {
		case s.sendEntered <- struct{}{}:
		default:
		}
	}
	if s.sendRelease != nil {
		<-s.sendRelease
	}
	return nil
}
func (s *fakeSubscribeStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeSubscribeStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeSubscribeStream) SetTrailer(metadata.MD)       {}
func (s *fakeSubscribeStream) Context() context.Context     { return s.ctx }
func (s *fakeSubscribeStream) SendMsg(any) error            { return nil }
func (s *fakeSubscribeStream) RecvMsg(any) error            { return nil }

func TestShutdownRejectsRequestsAndReleasesBlockedHandler(t *testing.T) {
	service := &Service{shutdown: make(chan struct{})}
	stream := &fakeSubscribeStream{ctx: context.Background(), sendEntered: make(chan struct{}, 1), sendRelease: make(chan struct{})}
	handlerDone := make(chan error, 1)
	go func() { handlerDone <- service.Subscribe(&pb.SubscribeReq{}, stream) }()
	select {
	case <-stream.sendEntered:
	case <-time.After(time.Second):
		t.Fatal("ACK send did not start")
	}
	service.BeginShutdown()
	select {
	case err := <-handlerDone:
		if err != nil {
			t.Fatalf("handler error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler waited for blocked send")
	}
	if _, err := service.Publish(context.Background(), nil); status.Code(err) != codes.Unavailable {
		t.Fatalf("Publish status = %v", err)
	}
	if err := service.Subscribe(&pb.SubscribeReq{}, &fakeSubscribeStream{ctx: context.Background()}); status.Code(err) != codes.Unavailable {
		t.Fatalf("Subscribe status = %v", err)
	}
	close(stream.sendRelease)
	senderDone := make(chan struct{})
	go func() { service.senders.Wait(); close(senderDone) }()
	select {
	case <-senderDone:
	case <-time.After(time.Second):
		t.Fatal("sender did not exit")
	}
	if n := stream.sends.Load(); n != 1 {
		t.Fatalf("send count = %d, want 1", n)
	}
}

func TestGracefulShutdownSendsStreamEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zerolog.Nop()
	service, err := NewService(ctx, &logger, nil, nil, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	server := grpc.NewServer()
	pb.RegisterMsgLakeServer(server, service)
	listener := bufconn.Listen(1024 * 1024)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	dialCtx, dialCancel := context.WithTimeout(ctx, 3*time.Second)
	defer dialCancel()
	conn, err := grpc.DialContext(dialCtx, "bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	key, err := crypto.GenPrivKey()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := key.Sign(protocol.SubscribeSigningBytes("shutdown-topic"))
	if err != nil {
		t.Fatal(err)
	}
	stream, err := pb.NewMsgLakeClient(conn).Subscribe(ctx, &pb.SubscribeReq{TopicId: "shutdown-topic", Signature: &pb.Signature{PubKey: key.PubKey().Bytes(), Data: sig}})
	if err != nil {
		t.Fatal(err)
	}
	ack, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if !ack.GetOk() {
		t.Fatal("subscription rejected")
	}
	service.BeginShutdown()
	graceful := make(chan struct{})
	go func() { server.GracefulStop(); close(graceful) }()
	recv := make(chan error, 1)
	go func() { _, err := stream.Recv(); recv <- err }()
	select {
	case err := <-recv:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Recv error = %v, want EOF", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stream did not reach EOF")
	}
	select {
	case <-graceful:
	case <-time.After(3 * time.Second):
		t.Fatal("GracefulStop blocked")
	}
	service.handlers.Wait()
	service.senders.Wait()
}

func TestShutdownMapsBoxClosedToEOFButKeepsSendError(t *testing.T) {
	service := &Service{shutdown: make(chan struct{})}
	if err := service.streamResult(msg.ErrBoxClosed); !errors.Is(err, msg.ErrBoxClosed) {
		t.Fatalf("before shutdown error = %v", err)
	}
	service.BeginShutdown()
	if err := service.streamResult(msg.ErrBoxClosed); err != nil {
		t.Fatalf("shutdown box error = %v, want EOF", err)
	}
	sendErr := errors.New("send failed")
	result := make(chan error, 1)
	result <- sendErr
	if err := service.shutdownStreamResult(result, nil); !errors.Is(err, sendErr) {
		t.Fatalf("send error = %v, want %v", err, sendErr)
	}
	result = make(chan error, 1)
	result <- msg.ErrBoxClosed
	if err := service.shutdownStreamResult(result, nil); err != nil {
		t.Fatalf("closed box result = %v, want EOF", err)
	}
	if got := status.Code(service.streamResult(msg.ErrSlowSubscriber)); got != codes.ResourceExhausted {
		t.Fatalf("slow subscriber status = %v", got)
	}
}

func TestShutdownReleasesHandlerDuringBlockedRelaySend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger := zerolog.Nop()
	service, err := NewService(ctx, &logger, nil, nil, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	box, err := service.relayer.GetMsgCenter().GetBox("blocked-relay")
	if err != nil {
		t.Fatal(err)
	}
	subscriber, err := box.JoinSub("test")
	if err != nil {
		t.Fatal(err)
	}
	defer box.LeaveSub("test")
	stream := &fakeSubscribeStream{ctx: ctx, relayEntered: make(chan struct{}, 1), relayRelease: make(chan struct{})}
	ack := &pb.SubscribeRes{Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_ACK, Res: &pb.SubscribeRes_Ok{Ok: true}}
	result := service.startSender(stream, ack, subscriber)
	key, err := crypto.GenPrivKey()
	if err != nil {
		t.Fatal(err)
	}
	capsule := &pb.MsgCapsule{TopicId: "blocked-relay", Data: []byte("message")}
	signing, err := protocol.MsgCapsuleSigningBytes(capsule)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := key.Sign(signing)
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Publish(&pb.SignedMsgCapsule{MsgCapsule: capsule, Signature: &pb.Signature{PubKey: key.PubKey().Bytes(), Data: signature}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stream.relayEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("relay Send did not start")
	}
	service.BeginShutdown()
	handlerDone := make(chan error, 1)
	go func() { handlerDone <- service.waitSender(stream, result, subscriber) }()
	select {
	case err := <-handlerDone:
		if err != nil {
			t.Fatalf("handler error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler waited for blocked relay Send")
	}
	close(stream.relayRelease)
	senderDone := make(chan struct{})
	go func() { service.senders.Wait(); close(senderDone) }()
	select {
	case <-senderDone:
	case <-time.After(time.Second):
		t.Fatal("sender did not exit")
	}
}

func TestAcceptedPublishDrainsDuringShutdownWhileGetBoxBlocks(t *testing.T) {
	logger := zerolog.Nop()
	entered := make(chan struct{})
	release := make(chan struct{})
	service := &Service{logger: &logger, shutdown: make(chan struct{})}
	wanted := errors.New("lookup released")
	service.getBoxFn = func(string) (*msg.Box, error) { close(entered); <-release; return nil, wanted }
	key, err := crypto.GenPrivKey()
	if err != nil {
		t.Fatal(err)
	}
	capsule := &pb.MsgCapsule{TopicId: "drain-topic", Data: []byte("message")}
	signing, err := protocol.MsgCapsuleSigningBytes(capsule)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := key.Sign(signing)
	if err != nil {
		t.Fatal(err)
	}
	req := &pb.PublishReq{SignedMsgCapsule: &pb.SignedMsgCapsule{MsgCapsule: capsule, Signature: &pb.Signature{PubKey: key.PubKey().Bytes(), Data: signature}}}
	published := make(chan error, 1)
	go func() { _, err := service.Publish(context.Background(), req); published <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Publish did not reach GetBox")
	}
	service.BeginShutdown()
	drained := make(chan struct{})
	go func() { service.publishes.Wait(); close(drained) }()
	select {
	case <-drained:
		t.Fatal("Publish drain finished before GetBox")
	default:
	}
	close(release)
	select {
	case err := <-published:
		if !errors.Is(err, wanted) {
			t.Fatalf("Publish error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Publish did not finish")
	}
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("Publish drain did not finish")
	}
	if _, err := service.Publish(context.Background(), req); status.Code(err) != codes.Unavailable {
		t.Fatalf("late Publish status = %v", err)
	}
}
