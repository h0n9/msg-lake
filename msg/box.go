package msg

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	pb "github.com/h0n9/msg-lake/proto"
)

type Box struct {
	ctx    context.Context
	cancel context.CancelFunc

	logger *zerolog.Logger
	wg     sync.WaitGroup

	topicID string
	topic   *pubsub.Topic

	// chans for operations
	setSubscriberCh    setSubscriberCh
	deleteSubscriberCh deleteSubscriberCh

	subCh     SubscriberCh
	sub       *pubsub.Subscription
	subCtx    context.Context
	subCancel context.CancelFunc

	subscribers map[string]SubscriberCh
}

func NewBox(logger *zerolog.Logger, topicID string, topic *pubsub.Topic) (*Box, error) {
	subLogger := logger.With().Str("module", "msg-box").Logger()
	ctx, cancel := context.WithCancel(context.Background())
	box := Box{
		ctx:    ctx,
		cancel: cancel,

		logger: &subLogger,
		wg:     sync.WaitGroup{},

		topicID: topicID,
		topic:   topic,

		setSubscriberCh:    make(setSubscriberCh),
		deleteSubscriberCh: make(deleteSubscriberCh),

		subCh:     make(SubscriberCh, 10),
		sub:       nil,
		subCtx:    nil,
		subCancel: nil,

		subscribers: make(map[string]SubscriberCh),
	}

	go func() {
		var (
			msgCapsule *pb.MsgCapsule

			setSubscriber    setSubscriber
			deleteSubscriber deleteSubscriber
		)
		for {
			select {
			case <-ctx.Done():
				return
			case msgCapsule = <-box.subCh:
				for subscriberID, subscriberCh := range box.subscribers {
					subLogger.Debug().Str("subscriber-id", subscriberID).Msg("relaying")
					subscriberCh <- msgCapsule
					subLogger.Debug().Str("subscriber-id", subscriberID).Msg("relayed")
				}
				msgCapsule = nil // explicitly free
			case setSubscriber = <-box.setSubscriberCh:
				_, exist := box.subscribers[setSubscriber.subscriberID]
				if exist {
					setSubscriber.errCh <- fmt.Errorf("%s is already subscribing", setSubscriber.subscriberID)
					continue
				}
				box.subscribers[setSubscriber.subscriberID] = setSubscriber.subscriberCh
				if box.sub == nil {
					go box.startSub()
				}
				setSubscriber.errCh <- nil
			case deleteSubscriber = <-box.deleteSubscriberCh:
				subscriberCh, exist := box.subscribers[deleteSubscriber.subscriberID]
				if !exist {
					deleteSubscriber.errCh <- fmt.Errorf("%s is not subscribing", <-deleteSubscriber.errCh)
					continue
				}
				close(subscriberCh)
				subLogger.Debug().Str("subscriber-id", deleteSubscriber.subscriberID).Msg("closed channel")
				delete(box.subscribers, deleteSubscriber.subscriberID)
				subLogger.Debug().Str("subscriber-id", deleteSubscriber.subscriberID).Msg("deleted channel")
				deleteSubscriber.errCh <- nil
			}
		}
	}()

	return &box, nil
}

func (box *Box) startSub() {
	sub, err := box.topic.Subscribe()
	if err != nil {
		box.logger.Err(err).Msg("")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	box.subCtx = ctx
	box.subCancel = cancel
	box.sub = sub
	box.logger.Debug().Str("topic-id", box.topicID).Msg("started subscription")
	for {
		pubSubMsg, err := box.sub.Next(box.subCtx)
		if err != nil {
			if errors.Is(context.Canceled, err) {
				sub.Cancel()
				box.subCtx = nil
				box.subCancel = nil
				box.sub = nil
				box.logger.Debug().Str("topic-id", box.topicID).Msg("stopped subscription")
				return
			}
			box.logger.Err(err).Msg("")
			continue
		}
		msgCapsule := pb.MsgCapsule{}
		err = proto.Unmarshal(pubSubMsg.GetData(), &msgCapsule)
		if err != nil {
			box.logger.Err(err).Msg("")
			continue
		}
		box.subCh <- &msgCapsule
	}
}

func (box *Box) StopSub() {
	box.subCancel()
}

func (box *Box) Close() error {
	// cancel context
	box.cancel()

	// close channels
	close(box.setSubscriberCh)
	close(box.deleteSubscriberCh)
	close(box.subCh)

	// clean up subscribers
	for id := range box.subscribers {
		delete(box.subscribers, id)
	}

	// cancel topic subscription
	box.StopSub()

	// close topic
	return box.topic.Close()
}

func (box *Box) Publish(msgCapsule *pb.MsgCapsule) error {
	msgCapsule.Timestamp = time.Now().UnixNano()
	data, err := proto.Marshal(msgCapsule)
	if err != nil {
		return err
	}
	return box.topic.Publish(box.ctx, data)
}

func (box *Box) JoinSub(subscriberID string) (SubscriberCh, error) {
	var (
		subscriberCh = make(SubscriberCh, 10)
		errCh        = make(chan error)
	)
	defer close(errCh)

	box.setSubscriberCh <- setSubscriber{
		subscriberID: subscriberID,
		subscriberCh: subscriberCh,

		errCh: errCh,
	}
	err := <-errCh
	if err != nil {
		close(subscriberCh)
		return nil, err
	}

	return subscriberCh, nil
}

func (box *Box) LeaveSub(subscriberID string) (int, error) {
	var (
		errCh = make(chan error)
	)
	defer close(errCh)

	box.deleteSubscriberCh <- deleteSubscriber{
		subscriberID: subscriberID,

		errCh: errCh,
	}
	err := <-errCh
	return len(box.subscribers), err
}
