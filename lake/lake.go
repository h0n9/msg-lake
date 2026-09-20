package lake

import (
	"context"
	"errors"
	"fmt"

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

	ctx     context.Context
	logger  *zerolog.Logger
	relayer *relayer.Relayer
}

func NewService(ctx context.Context, logger *zerolog.Logger, seed []byte, relayerAddrs []string, mdnsEnabled bool, dhtEnabled bool, bootstrapPeers []string) (*Service, error) {
	subLogger := logger.With().Str("module", "lake-service").Logger()
	relayer, err := relayer.NewRelayer(ctx, logger, seed, relayerAddrs, mdnsEnabled, dhtEnabled, bootstrapPeers)
	if err != nil {
		return nil, err
	}
	go relayer.DiscoverPeers()
	return &Service{
		ctx:     ctx,
		logger:  &subLogger,
		relayer: relayer,
	}, nil
}

func (service *Service) Close() {
	if service.relayer != nil {
		service.relayer.Close()
	}
}

func (service *Service) Publish(ctx context.Context, req *pb.PublishReq) (*pb.PublishRes, error) {
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
	msgCenter := service.relayer.GetMsgCenter()

	// get msg box
	msgBox, err := msgCenter.GetBox(topicID)
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
	service.logger.Debug().
		Str("topic-id", req.GetTopicId()).
		Msg("begin of subscribe stream")
	defer service.logger.Debug().
		Str("topic-id", req.GetTopicId()).
		Msg("end of subscribe stream")

	// set subscribe res
	res := pb.SubscribeRes{
		Type:    pb.SubscribeResType_SUBSCRIBE_RES_TYPE_ACK,
		TopicId: req.GetTopicId(),
		Res: &pb.SubscribeRes_Ok{
			Ok: false,
		},
	}

	// check constraints
	if !util.CheckStrLen(req.GetTopicId(), MinTopicIDLen, MaxTopicIDLen) {
		err := stream.Send(&res)
		if err != nil {
			return err
		}
		return nil
	}
	if err := protocol.VerifySubscribe(req.GetTopicId(), req.GetSignature()); err != nil {
		if sendErr := stream.Send(&res); sendErr != nil {
			return sendErr
		}
		return nil
	}
	// get msg center
	msgCenter := service.relayer.GetMsgCenter()

	// get msg box
	msgBox, err := msgCenter.GetBox(req.GetTopicId())
	if err != nil {
		err := stream.Send(&res)
		if err != nil {
			return err
		}
		return nil
	}

	// generate random subscriber id
	subscriberID := util.GenerateRandomBase64String(RandomSubscriberIDLen)

	// register subscriber id to msg box
	subscriber, err := msgBox.JoinSub(subscriberID)
	if err != nil {
		err := stream.Send(&res)
		if err != nil {
			return err
		}
		return nil
	}

	service.logger.Info().
		Str("topic-id", req.GetTopicId()).
		Str("subscriber-id", subscriberID).
		Msg("joined subscriber")
	defer func() {
		if err := msgBox.LeaveSub(subscriberID); err != nil {
			service.logger.Err(err).
				Str("topic-id", req.GetTopicId()).
				Str("subscriber-id", subscriberID).
				Msg("failed to leave subscriber")
		}
		service.logger.Info().
			Str("topic-id", req.GetTopicId()).
			Str("subscriber-id", subscriberID).
			Msg("left subscriber")
	}()

	// update subscriber res
	res.SubscriberId = subscriberID
	res.Res = &pb.SubscribeRes_Ok{Ok: true}

	// send subscriber res
	err = stream.Send(&res)
	if err != nil {
		return err
	}

	sendResultCh := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stream.Context().Done():
				sendResultCh <- stream.Context().Err()
				return
			case <-subscriber.Done():
				sendResultCh <- subscriber.Err()
				return
			case msgCapsule := <-subscriber.Messages():
				err := stream.Send(&pb.SubscribeRes{
					Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY,
					Res: &pb.SubscribeRes_TimestampedSignedMsgCapsule{
						TimestampedSignedMsgCapsule: msgCapsule,
					},
				})
				if err != nil {
					sendResultCh <- err
					return
				}
				msgCapsule = nil // explicitly free
			}
		}
	}()

	select {
	case <-stream.Context().Done():
		return nil
	case <-subscriber.Done():
		return subscriberStreamError(subscriber.Err())
	case err := <-sendResultCh:
		return subscriberStreamError(err)
	}
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
