package msg

import (
	"errors"
	"sync"

	pb "github.com/h0n9/msg-lake/proto"
)

type SubscriberCh chan *pb.MsgCapsule

var (
	ErrSlowSubscriber = errors.New("subscriber queue is full")
	ErrBoxClosed      = errors.New("message box is closed")
)

type Subscriber struct {
	messages SubscriberCh
	done     chan struct{}

	mu       sync.RWMutex
	err      error
	stopOnce sync.Once
}

func newSubscriber(bufferSize int) *Subscriber {
	return &Subscriber{
		messages: make(SubscriberCh, bufferSize),
		done:     make(chan struct{}),
	}
}

func (subscriber *Subscriber) Messages() <-chan *pb.MsgCapsule {
	return subscriber.messages
}

func (subscriber *Subscriber) Done() <-chan struct{} {
	return subscriber.done
}

func (subscriber *Subscriber) Err() error {
	subscriber.mu.RLock()
	defer subscriber.mu.RUnlock()
	return subscriber.err
}

func (subscriber *Subscriber) stop(err error) {
	subscriber.stopOnce.Do(func() {
		subscriber.mu.Lock()
		subscriber.err = err
		subscriber.mu.Unlock()
		close(subscriber.done)
	})
}

type setSubscriber struct {
	subscriberID string
	subscriber   *Subscriber

	errCh chan error
}

type deleteSubscriber struct {
	subscriberID string

	errCh chan error
}

type (
	setSubscriberCh    chan setSubscriber
	deleteSubscriberCh chan deleteSubscriber
)
