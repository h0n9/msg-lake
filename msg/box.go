package msg

import (
	"context"
	"fmt"
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/rs/zerolog"
)

type Box struct {
	ctx    context.Context
	logger *zerolog.Logger
	wg     sync.WaitGroup

	topicID string
	topic   *pubsub.Topic

	// chans for operations
	setSubscriberCh    setSubscriberCh
	deleteSubscriberCh deleteSubscriberCh

	subscriberCh SubscriberCh
	subscription *pubsub.Subscription
	subscribers  map[string]SubscriberCh
}

func NewBox(ctx context.Context, logger *zerolog.Logger, topicID string, topic *pubsub.Topic) (*Box, error) {
	subLogger := logger.With().Str("module", "msg-box").Logger()
	subscription, err := topic.Subscribe()
	if err != nil {
		return nil, err
	}
	box := Box{
		ctx:    ctx,
		logger: &subLogger,
		wg:     sync.WaitGroup{},

		topicID: topicID,
		topic:   topic,

		setSubscriberCh:    make(setSubscriberCh),
		deleteSubscriberCh: make(deleteSubscriberCh),

		subscriberCh: make(SubscriberCh, 100),
		subscription: subscription,
		subscribers:  make(map[string]SubscriberCh),
	}
	box.wg.Add(1)
	go func() {
		defer box.wg.Done()
		var (
			msg []byte

			setSubscriber    setSubscriber
			deleteSubscriber deleteSubscriber
		)
		for {
			select {
			case msg = <-box.subscriberCh:
				for subscriberID, subscriberCh := range box.subscribers {
					subLogger.Debug().Str("subscriber-id", subscriberID).Msg("relaying")
					subscriberCh <- msg
					subLogger.Debug().Str("subscriber-id", subscriberID).Msg("relayed")
				}
			case setSubscriber = <-box.setSubscriberCh:
				_, exist := box.subscribers[setSubscriber.subscriberID]
				if exist {
					setSubscriber.errCh <- fmt.Errorf("%s is already subscribing", setSubscriber.subscriberID)
					continue
				}
				box.subscribers[setSubscriber.subscriberID] = setSubscriber.subscriberCh
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

	box.wg.Add(1)
	go func() {
		defer box.wg.Done()
		defer subscription.Cancel()
		for {
			msg, err := subscription.Next(ctx)
			if err != nil {
				subLogger.Err(err).Msg("")
				return
			}
			box.subscriberCh <- msg.GetData()
		}
	}()
	return &box, nil
}

func (box *Box) Publish(data []byte) error {
	return box.topic.Publish(box.ctx, data)
}

func (box *Box) Subscribe(subscriberID string) (SubscriberCh, error) {
	var (
		subscriberCh = make(SubscriberCh, 100)
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

func (box *Box) StopSubscription(subscriberID string) error {
	var (
		errCh = make(chan error)
	)
	defer close(errCh)

	box.deleteSubscriberCh <- deleteSubscriber{
		subscriberID: subscriberID,

		errCh: errCh,
	}
	return <-errCh
}
