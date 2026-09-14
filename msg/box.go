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
	"github.com/h0n9/msg-lake/util"
)

const (
	DefaultInternalChanBufferSize = 5000
	DefaultExternalChanBufferSize = 256
)

var (
	internalChanBufferSize int
	externalChanBufferSize int
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

	subscribers map[string]*Subscriber
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

		subCh:     make(SubscriberCh, internalChanBufferSize),
		sub:       nil,
		subCtx:    nil,
		subCancel: nil,

		subscribers: make(map[string]*Subscriber),
	}

	box.wg.Add(1)
	go func() {
		defer box.wg.Done()
		for {
			select {
			case <-ctx.Done():
				for _, subscriber := range box.subscribers {
					subscriber.stop(ErrBoxClosed)
				}
				return
			case msgCapsule := <-box.subCh:
				for subscriberID, subscriber := range box.subscribers {
					select {
					case subscriber.messages <- msgCapsule:
					default:
						delete(box.subscribers, subscriberID)
						subscriber.stop(ErrSlowSubscriber)
						box.logger.Warn().
							Str("topic-id", box.topicID).
							Str("subscriber-id", subscriberID).
							Int("queue-length", len(subscriber.messages)).
							Int("queue-capacity", cap(subscriber.messages)).
							Msg("removed slow subscriber")
					}
				}
				if len(box.subscribers) == 0 {
					box.StopSub()
				}
				msgCapsule = nil // explicitly free
			case setSubscriber := <-box.setSubscriberCh:
				_, exist := box.subscribers[setSubscriber.subscriberID]
				if exist {
					setSubscriber.errCh <- fmt.Errorf("%s is already subscribing", setSubscriber.subscriberID)
					continue
				}
				box.subscribers[setSubscriber.subscriberID] = setSubscriber.subscriber
				if box.sub == nil {
					go box.startSub()
				}
				setSubscriber.errCh <- nil
			case deleteSubscriber := <-box.deleteSubscriberCh:
				subscriber, exist := box.subscribers[deleteSubscriber.subscriberID]
				if !exist {
					deleteSubscriber.errCh <- nil
					continue
				}
				delete(box.subscribers, deleteSubscriber.subscriberID)
				subscriber.stop(nil)
				subLogger.Debug().
					Str("topic-id", topicID).
					Str("subscriber-id", deleteSubscriber.subscriberID).
					Msg("deleted channel")
				box.logger.Debug().
					Str("topic-id", box.topicID).
					Int("num-of-subscribers", len(box.subscribers)).
					Msg("")
				if len(box.subscribers) == 0 {
					box.StopSub()
				}
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
	box.logger.Info().
		Str("topic-id", box.topicID).
		Msg("started subscription")

	wg := sync.WaitGroup{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				pubSubMsg, err := box.sub.Next(box.subCtx)
				if err != nil {
					if errors.Is(context.Canceled, err) {
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
	}()

	wg.Wait()

	sub.Cancel()
	box.subCtx = nil
	box.subCancel = nil
	box.sub = nil
	box.logger.Info().
		Str("topic-id", box.topicID).
		Msg("stopped subscription")
}

func (box *Box) StopSub() {
	if box.subCancel == nil {
		return
	}
	box.subCancel()
}

func (box *Box) Close() error {
	// cancel context
	box.cancel()
	box.wg.Wait()

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

func (box *Box) JoinSub(subscriberID string) (*Subscriber, error) {
	var (
		subscriber = newSubscriber(externalChanBufferSize)
		errCh      = make(chan error, 1)
	)

	select {
	case <-box.ctx.Done():
		subscriber.stop(ErrBoxClosed)
		return nil, ErrBoxClosed
	case box.setSubscriberCh <- setSubscriber{
		subscriberID: subscriberID,
		subscriber:   subscriber,
		errCh:        errCh,
	}:
	}
	var err error
	select {
	case <-box.ctx.Done():
		subscriber.stop(ErrBoxClosed)
		return nil, ErrBoxClosed
	case err = <-errCh:
	}
	if err != nil {
		subscriber.stop(err)
		return nil, err
	}

	return subscriber, nil
}

func (box *Box) LeaveSub(subscriberID string) error {
	var (
		errCh = make(chan error, 1)
	)

	select {
	case <-box.ctx.Done():
		return nil
	case box.deleteSubscriberCh <- deleteSubscriber{
		subscriberID: subscriberID,

		errCh: errCh,
	}:
	}
	var err error
	select {
	case <-box.ctx.Done():
		return nil
	case err = <-errCh:
	}
	if err != nil {
		return err
	}
	return nil
}

func init() {
	tmp, err := util.GetEnvInt("INTERNAL_CHAN_BUFFER_SIZE", DefaultInternalChanBufferSize)
	if err != nil {
		panic(err)
	}
	internalChanBufferSize = tmp

	externalChanBufferSize = loadExternalChanBufferSize()
}

func loadExternalChanBufferSize() int {
	size, err := util.GetEnvInt("EXTERNAL_CHAN_BUFFER_SIZE", DefaultExternalChanBufferSize)
	if err != nil {
		panic(err)
	}
	if size <= 0 {
		panic("EXTERNAL_CHAN_BUFFER_SIZE must be greater than zero")
	}
	return size
}
