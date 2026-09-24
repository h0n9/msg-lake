package lake

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/rs/zerolog"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/postie-labs/go-postie-lib/crypto"

	"github.com/h0n9/msg-lake/msg"
	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/protocol"
	"github.com/h0n9/msg-lake/relayer"
	"github.com/h0n9/msg-lake/util"
)

const (
	MaxTopicIDLen = 64
	MinTopicIDLen = 1

	RandomSubscriberIDLen = 10
)

type Service struct {
	pb.UnimplementedMsgLakeServer

	ctx          context.Context
	logger       *zerolog.Logger
	relayer      *relayer.Relayer
	mu           sync.Mutex
	shuttingDown bool
	shutdown     chan struct{}
	publishes    sync.WaitGroup
	handlers     sync.WaitGroup
	senders      sync.WaitGroup
	getBoxFn     func(string) (*msg.Box, error)
}

func NewService(ctx context.Context, logger *zerolog.Logger, seed []byte, relayerAddrs []string, mdnsEnabled bool, dhtEnabled bool, bootstrapPeers []string) (*Service, error) {
	subLogger := logger.With().Str("module", "lake-service").Logger()
	relayer, err := relayer.NewRelayer(ctx, logger, seed, relayerAddrs, mdnsEnabled, dhtEnabled, bootstrapPeers)
	if err != nil {
		return nil, err
	}
	if mdnsEnabled {
		go relayer.DiscoverPeers()
	}
	return &Service{
		ctx:      ctx,
		logger:   &subLogger,
		relayer:  relayer,
		shutdown: make(chan struct{}),
	}, nil
}

func (service *Service) Close() error {
	service.BeginShutdown()
	service.publishes.Wait()
	service.handlers.Wait()
	service.senders.Wait()
	if service.relayer != nil {
		return service.relayer.Close()
	}
	return nil
}

func (service *Service) BeginShutdown() {
	service.mu.Lock()
	if !service.shuttingDown {
		service.shuttingDown = true
		close(service.shutdown)
	}
	service.mu.Unlock()
}

func (service *Service) CancelBackend() {
	if service.relayer != nil {
		service.relayer.CancelBackend()
	}
}

func (service *Service) admit(publish bool) bool {
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.shuttingDown {
		return false
	}
	if publish {
		service.publishes.Add(1)
	} else {
		service.handlers.Add(1)
	}
	return true
}

func (service *Service) Publish(ctx context.Context, req *pb.PublishReq) (*pb.PublishRes, error) {
	if !service.admit(true) {
		return nil, status.Error(codes.Unavailable, "service is shutting down")
	}
	defer service.publishes.Done()
	signedMsgCapsule := req.GetSignedMsgCapsule()
	msgCapsule := signedMsgCapsule.GetMsgCapsule()
	topicID := msgCapsule.GetTopicId()

	// set publish res
	publishRes := pb.PublishRes{
		TopicId: topicID,
		Ok:      false,
	}

	// check constraints
	if !util.CheckStrLen(topicID, MinTopicIDLen, MaxTopicIDLen) {
		return &publishRes, fmt.Errorf("failed to verify length of topic id")
	}
	if err := protocol.VerifySignedMsgCapsule(signedMsgCapsule); err != nil {
		return &publishRes, fmt.Errorf("failed to verify signed msg capsule: %w", err)
	}
	pubKey, err := crypto.GenPubKeyFromBytes(signedMsgCapsule.GetSignature().GetPubKey())
	if err != nil {
		return &publishRes, err
	}

	// get msg center
	// get msg box
	var msgBox *msg.Box
	if service.getBoxFn != nil {
		msgBox, err = service.getBoxFn(topicID)
	} else {
		msgBox, err = service.relayer.GetMsgCenter().GetBox(topicID)
	}
	if err != nil {
		return &publishRes, err
	}

	// publish msg
	err = msgBox.Publish(signedMsgCapsule)
	if err != nil {
		return &publishRes, err
	}

	service.logger.Debug().
		Str("topic-id", topicID).
		Str("addr", pubKey.Address().String()).
		Msg("published")

	// update publish res
	publishRes.Ok = true

	return &publishRes, nil
}
func (service *Service) Subscribe(req *pb.SubscribeReq, stream pb.MsgLake_SubscribeServer) error {
	if !service.admit(false) {
		return status.Error(codes.Unavailable, "service is shutting down")
	}
	defer service.handlers.Done()
	return service.subscribe(req, stream)
}

func (service *Service) subscribe(req *pb.SubscribeReq, stream pb.MsgLake_SubscribeServer) error {
	fail := &pb.SubscribeRes{Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_ACK, TopicId: req.GetTopicId(), Res: &pb.SubscribeRes_Ok{Ok: false}}
	if !util.CheckStrLen(req.GetTopicId(), MinTopicIDLen, MaxTopicIDLen) || protocol.VerifySubscribe(req.GetTopicId(), req.GetSignature()) != nil {
		return service.waitSender(stream, service.startSender(stream, fail, nil), nil)
	}
	box, err := service.relayer.GetMsgCenter().GetBox(req.GetTopicId())
	if err != nil {
		return service.waitSender(stream, service.startSender(stream, fail, nil), nil)
	}
	subscriberID := util.GenerateRandomBase64String(RandomSubscriberIDLen)
	joinCtx, cancelJoin := context.WithCancel(stream.Context())
	joinDone := make(chan struct{})
	go func() {
		select {
		case <-service.shutdown:
			cancelJoin()
		case <-joinDone:
		}
	}()
	subscriber, err := box.JoinSubContext(joinCtx, subscriberID)
	close(joinDone)
	cancelJoin()
	if err != nil {
		select {
		case <-service.shutdown:
			return nil
		case <-stream.Context().Done():
			return nil
		default:
		}
		return service.waitSender(stream, service.startSender(stream, fail, nil), nil)
	}
	defer box.LeaveSub(subscriberID)
	ack := &pb.SubscribeRes{Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_ACK, TopicId: req.GetTopicId(), SubscriberId: subscriberID, Res: &pb.SubscribeRes_Ok{Ok: true}}
	return service.waitSender(stream, service.startSender(stream, ack, subscriber), subscriber)
}

func (service *Service) startSender(stream pb.MsgLake_SubscribeServer, ack *pb.SubscribeRes, subscriber *msg.Subscriber) <-chan error {
	result := make(chan error, 1)
	service.senders.Add(1)
	go func() {
		defer service.senders.Done()
		defer close(result)
		send := func(response *pb.SubscribeRes) error {
			// A send admitted just before shutdown may still finish. No queue item is
			// taken after shutdown has been observed.
			select {
			case <-service.shutdown:
				return nil
			default:
			}
			return stream.Send(response)
		}
		if err := send(ack); err != nil {
			result <- err
			return
		}
		if subscriber == nil {
			result <- nil
			return
		}
		for {
			select {
			case <-service.shutdown:
				result <- nil
				return
			default:
			}
			select {
			case <-service.shutdown:
				result <- nil
				return
			case <-stream.Context().Done():
				result <- stream.Context().Err()
				return
			case <-subscriber.Done():
				result <- subscriber.Err()
				return
			case capsule := <-subscriber.Messages():
				select {
				case <-service.shutdown:
					result <- nil
					return
				default:
				}
				if err := send(&pb.SubscribeRes{Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY, Res: &pb.SubscribeRes_TimestampedSignedMsgCapsule{TimestampedSignedMsgCapsule: capsule}}); err != nil {
					result <- err
					return
				}
			}
		}
	}()
	return result
}

func (service *Service) waitSender(stream pb.MsgLake_SubscribeServer, result <-chan error, subscriber *msg.Subscriber) error {
	select {
	case <-service.shutdown:
		return service.shutdownStreamResult(result, subscriber)
	case <-stream.Context().Done():
		return nil
	case err := <-result:
		return service.streamResult(err)
	case <-subscriberDone(subscriber):
		return service.streamResult(subscriber.Err())
	}
}

func (service *Service) streamResult(err error) error {
	if errors.Is(err, msg.ErrBoxClosed) {
		select {
		case <-service.shutdown:
			return nil
		default:
		}
	}
	return subscriberStreamError(err)
}

func (service *Service) shutdownStreamResult(result <-chan error, subscriber *msg.Subscriber) error {
	if subscriber != nil {
		if err := subscriber.Err(); err != nil && !errors.Is(err, msg.ErrBoxClosed) {
			return subscriberStreamError(err)
		}
	}
	select {
	case err := <-result:
		if err != nil && !errors.Is(err, msg.ErrBoxClosed) {
			return subscriberStreamError(err)
		}
	default:
	}
	return nil
}

func subscriberDone(subscriber *msg.Subscriber) <-chan struct{} {
	if subscriber == nil {
		return nil
	}
	return subscriber.Done()
}

func subscriberStreamError(err error) error {
	if errors.Is(err, msg.ErrSlowSubscriber) {
		return status.Error(codes.ResourceExhausted, "subscriber queue is full")
	}
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
