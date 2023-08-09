package msg

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	DefaultExternalChanBufferSize = 1000
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

		subCh:     make(SubscriberCh, internalChanBufferSize),
		sub:       nil,
		subCtx:    nil,
		subCancel: nil,

		subscribers: make(map[string]SubscriberCh),
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msgCapsule := <-box.subCh:
				for _, subscriberCh := range box.subscribers {
					if len(subscriberCh) == externalChanBufferSize {
						// when subscriberCh is full, just skip this time
						continue
					}
					subscriberCh <- msgCapsule
				}
				msgCapsule = nil // explicitly free
			case setSubscriber := <-box.setSubscriberCh:
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
			case deleteSubscriber := <-box.deleteSubscriberCh:
				subscriberCh, exist := box.subscribers[deleteSubscriber.subscriberID]
				if !exist {
					deleteSubscriber.errCh <- fmt.Errorf("%s is not subscribing", <-deleteSubscriber.errCh)
					continue
				}
				close(subscriberCh)
				subLogger.Debug().
					Str("topic-id", topicID).
					Str("subscriber-id", deleteSubscriber.subscriberID).
					Msg("closed channel")
				delete(box.subscribers, deleteSubscriber.subscriberID)
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
		subscriberCh = make(SubscriberCh, externalChanBufferSize)
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

func (box *Box) LeaveSub(subscriberID string) error {
	var (
		errCh = make(chan error)
	)
	defer close(errCh)

	box.deleteSubscriberCh <- deleteSubscriber{
		subscriberID: subscriberID,

		errCh: errCh,
	}
	err := <-errCh
	if err != nil {
		return err
	}
	return nil
}

func init() {
	tmp, err := getEnvInt("INTERNAL_CHAN_BUFFER_SIZE", DefaultInternalChanBufferSize)
	if err != nil {
		panic(err)
	}
	internalChanBufferSize = tmp

	tmp, err = getEnvInt("EXTERNAL_CHAN_BUFFER_SIZE", DefaultExternalChanBufferSize)
	if err != nil {
		panic(err)
	}
	externalChanBufferSize = tmp
}

func getEnvInt(key string, fallback int) (int, error) {
	tmpStr := strconv.Itoa(fallback)
	tmpStr = util.GetEnv(key, tmpStr)
	return strconv.Atoi(tmpStr)
}
