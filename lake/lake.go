package lake

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog"

	"github.com/postie-labs/go-postie-lib/crypto"

	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/relayer"
	"github.com/h0n9/msg-lake/util"
)

const (
	MaxTopicIDLen = 30
	MinTopicIDLen = 1

	RandomSubscriberIDLen = 10
)

type Service struct {
	pb.UnimplementedLakeServer

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
	service.logger.Info().Msg("closed lake service")
}

func (service *Service) Publish(ctx context.Context, req *pb.PublishReq) (*pb.PublishRes, error) {
	// set publish res
	publishRes := pb.PublishRes{
		TopicId: req.GetTopicId(),
		Ok:      false,
	}

	// check constraints
	if !util.CheckStrLen(req.GetTopicId(), MinTopicIDLen, MaxTopicIDLen) {
		return &publishRes, fmt.Errorf("failed to verify length of topic id")
	}
	pubKey, err := crypto.GenPubKeyFromBytes(req.GetMsgCapsule().GetSignature().GetPubKey())
	if err != nil {
		return &publishRes, err
	}
	if !pubKey.Verify(
		req.GetMsgCapsule().GetData(),
		req.GetMsgCapsule().GetSignature().GetData(),
	) {
		return &publishRes, fmt.Errorf("failed to verify signed data")
	}

	// get msg center
	msgCenter := service.relayer.GetMsgCenter()

	// get msg box
	msgBox, err := msgCenter.GetBox(req.GetTopicId())
	if err != nil {
		return &publishRes, err
	}

	// publish msg
	err = msgBox.Publish(req.GetMsgCapsule())
	if err != nil {
		return &publishRes, err
	}

	service.logger.Debug().
		Str("topic-id", req.GetTopicId()).
		Str("addr", string(pubKey.Address())).
		Msg("published")

	// update publish res
	publishRes.Ok = true

	return &publishRes, nil
}
func (service *Service) Subscribe(req *pb.SubscribeReq, stream pb.Lake_SubscribeServer) error {
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
	pubKeyBytes := req.MsgCapsule.GetSignature().GetPubKey()
	pubKey, err := crypto.GenPubKeyFromBytes(pubKeyBytes)
	if err != nil {
		err := stream.Send(&res)
		if err != nil {
			return err
		}
		return nil
	}
	if !pubKey.Verify(
		req.GetMsgCapsule().GetData(),
		req.GetMsgCapsule().GetSignature().GetData(),
	) {
		err := stream.Send(&res)
		if err != nil {
			return err
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
	subscriberCh, err := msgBox.JoinSub(subscriberID)
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

	// update subscriber res
	res.SubscriberId = subscriberID
	res.Res = &pb.SubscribeRes_Ok{Ok: true}

	// send subscriber res
	err = stream.Send(&res)
	if err != nil {
		return err
	}

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stream.Context().Done():
				return
			case msgCapsule := <-subscriberCh:
				err := stream.Send(&pb.SubscribeRes{
					Type: pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY,
					Res: &pb.SubscribeRes_MsgCapsule{
						MsgCapsule: msgCapsule,
					},
				})
				if err != nil {
					return
				}
				msgCapsule = nil // explicitly free
			}
		}
	}()

	wg.Wait()

	go func() {
		for len(subscriberCh) > 0 {
			<-subscriberCh
		}
		service.logger.Debug().
			Str("topic-id", req.GetTopicId()).
			Str("subscriber-id", subscriberID).
			Msg("drained subscriber ch")
	}()

	err = msgBox.LeaveSub(subscriberID)
	if err != nil {
		service.logger.Err(err).
			Str("topic-id", req.GetTopicId()).
			Str("subscriber-id", subscriberID).
			Msg("")
	}
	service.logger.Info().
		Str("topic-id", req.GetTopicId()).
		Str("subscriber-id", subscriberID).
		Msg("left subscriber")

	return nil
}
