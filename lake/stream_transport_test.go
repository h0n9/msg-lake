package lake

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/h0n9/msg-lake/msg"
	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/protocol"
	"github.com/postie-labs/go-postie-lib/crypto"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// These observers delegate every Send to the real gRPC transport. They never
// gate a Send or synthesize cancellation. Completion is observed before teardown.
type transportObservation struct {
	goroutine   string
	entered     atomic.Int32
	returned    atomic.Int32
	ackReturned chan struct{}
	sendFailed  chan error
	handlerDone chan error
}
type observedTransportStream struct {
	pb.MsgLake_SubscribeServer
	observation *transportObservation
}

func (s *observedTransportStream) Send(res *pb.SubscribeRes) error {
	o := s.observation
	if o.entered.Load() == 0 {
		stack := make([]byte, 128)
		n := runtime.Stack(stack, false)
		o.goroutine = strings.Fields(string(stack[:n]))[1]
	}
	o.entered.Add(1)
	err := s.MsgLake_SubscribeServer.Send(res)
	o.returned.Add(1)
	if res.GetType() == pb.SubscribeResType_SUBSCRIBE_RES_TYPE_ACK {
		close(o.ackReturned)
	}
	if err != nil {
		o.sendFailed <- err
	}
	return err
}

type observedTransportService struct {
	pb.UnimplementedMsgLakeServer
	service      *Service
	observations sync.Map
}

func (s *observedTransportService) Subscribe(req *pb.SubscribeReq, stream pb.MsgLake_SubscribeServer) error {
	md, _ := metadata.FromIncomingContext(stream.Context())
	ids := md.Get("test-id")
	if len(ids) != 1 {
		return status.Error(codes.Internal, "missing test-id")
	}
	value, ok := s.observations.Load(ids[0])
	if !ok {
		return status.Error(codes.Internal, "missing observer")
	}
	o := value.(*transportObservation)
	err := s.service.Subscribe(req, &observedTransportStream{stream, o})
	o.handlerDone <- err
	return err
}
func transportAwait[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
	var zero T
	return zero
}

func transportStacks() []string {
	size := 1 << 20
	for {
		stack := make([]byte, size)
		n := runtime.Stack(stack, true)
		if n < len(stack) {
			return strings.Split(string(stack[:n]), "\n\n")
		}
		size *= 2
	}
}
func awaitTransportGoroutineExit(t *testing.T, id, what string) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		present := false
		for _, stack := range transportStacks() {
			if strings.HasPrefix(stack, "goroutine "+id+" ") {
				present = true
				break
			}
		}
		if !present {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("%s goroutine %s retained", what, id)
		case <-tick.C:
		}
	}
}

// Inspect the particular sender, not total goroutine counts or elapsed sleep.
// This assertion intentionally depends on the pinned grpc-go implementation:
// writeQuota.get means SendMsg is waiting for HTTP/2 flow-control progress.
func awaitTransportBlocked(t *testing.T, o *transportObservation) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		for _, goroutine := range transportStacks() {
			if strings.HasPrefix(goroutine, "goroutine "+o.goroutine+" ") && strings.Contains(goroutine, "(*writeQuota).get") && strings.Contains(goroutine, "(*observedTransportStream).Send") {
				if o.entered.Load() <= o.returned.Load() {
					t.Fatal("no outstanding Send")
				}
				return
			}
		}
		select {
		case err := <-o.handlerDone:
			t.Fatalf("handler exited before transport blocked: %v", err)
		case <-deadline.C:
			t.Fatal("sender never blocked in actual gRPC writeQuota.get")
		case <-tick.C:
		}
	}
}

func TestBlockedTransportSendLifecycle(t *testing.T) {
	for _, transport := range []string{"bufconn", "tcp"} {
		t.Run(transport, func(t *testing.T) {
			for _, mode := range []string{"cancel", "disconnect", "eviction"} {
				t.Run(mode, func(t *testing.T) {
					ctx, cancelService := context.WithCancel(context.Background())
					logger := zerolog.Nop()
					service, err := NewService(ctx, &logger, nil, nil, false, false, nil)
					if err != nil {
						t.Fatal(err)
					}
					observer := &observedTransportService{service: service}
					var listener net.Listener
					var dialer func(context.Context, string) (net.Conn, error)
					if transport == "bufconn" {
						memory := bufconn.Listen(1 << 20)
						listener = memory
						dialer = func(context.Context, string) (net.Conn, error) { return memory.Dial() }
					} else {
						listener, err = net.Listen("tcp", "127.0.0.1:0")
						if err != nil {
							t.Fatal(err)
						}
						dialer = func(ctx context.Context, _ string) (net.Conn, error) {
							return (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
						}
					}
					server := grpc.NewServer()
					pb.RegisterMsgLakeServer(server, observer)
					go func() { _ = server.Serve(listener) }()
					t.Cleanup(func() { server.Stop(); _ = listener.Close(); cancelService(); _ = service.Close() })
					dial := func() *grpc.ClientConn {
						t.Helper()
						c, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(dialer), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStaticStreamWindowSize(65535), grpc.WithStaticConnWindowSize(65535))
						if err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = c.Close() })
						return c
					}
					key, err := crypto.GenPrivKey()
					if err != nil {
						t.Fatal(err)
					}
					const topic = "transport-lifecycle"
					box, err := service.relayer.GetMsgCenter().GetBox(topic)
					if err != nil {
						t.Fatal(err)
					}
					signature, err := key.Sign(protocol.SubscribeSigningBytes(topic))
					if err != nil {
						t.Fatal(err)
					}
					req := &pb.SubscribeReq{TopicId: topic, Signature: &pb.Signature{PubKey: key.PubKey().Bytes(), Data: signature}}
					subscribe := func(c *grpc.ClientConn, id string) (pb.MsgLake_SubscribeClient, context.CancelFunc, *transportObservation, string) {
						t.Helper()
						o := &transportObservation{ackReturned: make(chan struct{}), sendFailed: make(chan error, 1), handlerDone: make(chan error, 1)}
						observer.observations.Store(id, o)
						rpcCtx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(ctx, "test-id", id), 30*time.Second)
						t.Cleanup(cancel)
						stream, err := pb.NewMsgLakeClient(c).Subscribe(rpcCtx, req)
						if err != nil {
							t.Fatal(err)
						}
						ack, err := stream.Recv()
						if err != nil || !ack.GetOk() {
							t.Fatalf("ACK = %v, %v", ack, err)
						}
						// Synchronize the immutable sender identifier with ACK completion.
						transportAwait(t, o.ackReturned, "ACK Send completion")
						return stream, cancel, o, ack.GetSubscriberId()
					}
					sharedConnection := dial()
					healthy, cancelHealthy, healthyObservation, _ := subscribe(sharedConnection, "healthy")
					workers := transportStacks()
					var workerID string
					for _, stack := range workers {
						if strings.Contains(stack, "github.com/h0n9/msg-lake/msg.(*Box).startWorker.func1") {
							if workerID != "" {
								t.Fatal("ambiguous subscription worker observation")
							}
							workerID = strings.Fields(stack)[1]
						}
					}
					if workerID == "" {
						t.Fatal("subscription worker not observed")
					}
					sequence := 0
					publish := func() {
						t.Helper()
						sequence++
						data := make([]byte, 256<<10)
						copy(data, fmt.Sprint(sequence))
						capsule := &pb.MsgCapsule{TopicId: topic, Data: data}
						signing, err := protocol.MsgCapsuleSigningBytes(capsule)
						if err != nil {
							t.Fatal(err)
						}
						sig, err := key.Sign(signing)
						if err != nil {
							t.Fatal(err)
						}
						if err := box.Publish(&pb.SignedMsgCapsule{MsgCapsule: capsule, Signature: &pb.Signature{PubKey: key.PubKey().Bytes(), Data: sig}}); err != nil {
							t.Fatal(err)
						}
						response, err := healthy.Recv()
						if err != nil || response.GetType() != pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY || !bytes.Equal(response.GetTimestampedSignedMsgCapsule().GetSignedMsgCapsule().GetMsgCapsule().GetData(), data) {
							t.Fatalf("healthy delivery failed: %v", err)
						}
					}
					rounds, clients := 100, 1
					if mode == "disconnect" {
						rounds = 20
					}
					if mode == "eviction" {
						rounds, clients = 3, 3
					}
					for round := 0; round < rounds; round++ {
						observations := make([]*transportObservation, clients)
						cancels := make([]context.CancelFunc, clients)
						connections := make([]*grpc.ClientConn, clients)
						ids := make([]string, clients)
						for i := range clients {
							if mode == "cancel" {
								connections[i] = sharedConnection
							} else {
								connections[i] = dial()
							}
							_, cancels[i], observations[i], ids[i] = subscribe(connections[i], fmt.Sprintf("%d-%d", round, i))
						}
						for range 4 {
							publish()
						}
						for _, o := range observations {
							awaitTransportBlocked(t, o)
						}
						if mode == "eviction" {
							// Fill the bounded queue while each Send is proven transport-blocked.
							// Pace on healthy delivery so that only stalled readers are evicted.
							for range msg.DefaultExternalChanBufferSize + 1 {
								publish()
							}
						} else {
							for i := range clients {
								if mode == "cancel" {
									cancels[i]()
								} else {
									_ = connections[i].Close()
								}
							}
						}
						for i, o := range observations {
							handlerErr := transportAwait(t, o.handlerDone, "handler completion")
							if mode == "eviction" && status.Code(handlerErr) != codes.ResourceExhausted {
								t.Fatalf("eviction status = %v", handlerErr)
							}
							if mode != "eviction" && handlerErr != nil {
								t.Fatalf("canceled handler = %v", handlerErr)
							}
							if err := transportAwait(t, o.sendFailed, "blocked Send returning"); err == nil {
								t.Fatal("blocked Send succeeded after termination")
							}
							awaitTransportGoroutineExit(t, o.goroutine, "sender")
							// Successful reuse proves the dispatcher removed this exact subscriber.
							joinCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
							sub, err := box.JoinSubContext(joinCtx, ids[i])
							cancel()
							if err != nil {
								t.Fatalf("subscriber retained: %v", err)
							}
							if err := box.LeaveSub(ids[i]); err != nil {
								t.Fatal(err)
							}
							transportAwait(t, sub.Done(), "probe subscriber removal")
							cancels[i]()
							if mode != "cancel" {
								_ = connections[i].Close()
							}
							observer.observations.Delete(fmt.Sprintf("%d-%d", round, i))
						}
						publish() // The service and healthy stream still work after cleanup.
					}
					cancelHealthy()
					transportAwait(t, healthyObservation.handlerDone, "healthy handler completion")
					drained := make(chan struct{})
					go func() { service.subscribeHandlers.Wait(); service.senders.Wait(); close(drained) }()
					transportAwait(t, drained, "all handlers and sender goroutines (before service shutdown)")
					awaitTransportGoroutineExit(t, workerID, "subscription worker")
					select {
					case <-service.shutdown:
						t.Fatal("validation required service shutdown")
					default:
					}
					// Center intentionally retains this idle Box for the service lifetime.
					retained, err := service.relayer.GetMsgCenter().GetBox(topic)
					if err != nil || retained != box {
						t.Fatal("idle Box was not retained")
					}
				})
			}
		})
	}
}
