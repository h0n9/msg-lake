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

type topicBackend interface {
	Subscribe() (subscriptionBackend, error)
	Publish(context.Context, []byte) error
	Close() error
}

type subscriptionBackend interface {
	Next(context.Context) ([]byte, error)
	Cancel()
}

type libp2pTopicAdapter struct{ topic *pubsub.Topic }

func (adapter *libp2pTopicAdapter) Subscribe() (subscriptionBackend, error) {
	subscription, err := adapter.topic.Subscribe()
	if err != nil {
		return nil, err
	}
	return &libp2pSubscriptionAdapter{subscription: subscription}, nil
}

func (adapter *libp2pTopicAdapter) Publish(ctx context.Context, data []byte) error {
	return adapter.topic.Publish(ctx, data)
}

func (adapter *libp2pTopicAdapter) Close() error { return adapter.topic.Close() }

type libp2pSubscriptionAdapter struct{ subscription *pubsub.Subscription }

func (adapter *libp2pSubscriptionAdapter) Next(ctx context.Context) ([]byte, error) {
	message, err := adapter.subscription.Next(ctx)
	if err != nil {
		return nil, err
	}
	return message.GetData(), nil
}

func (adapter *libp2pSubscriptionAdapter) Cancel() { adapter.subscription.Cancel() }

type subscriptionState uint8

const (
	subscriptionIdle subscriptionState = iota
	subscriptionStarting
	subscriptionRunning
	subscriptionStopping
	subscriptionClosing
)

type joinResult struct {
	subscriber *Subscriber
	err        error
}

type joinRequest struct {
	subscriberID string
	subscriber   *Subscriber
	reply        chan joinResult
	ctx          context.Context
}

type cancelJoinRequest struct {
	subscriberID string
	subscriber   *Subscriber
}

type leaveRequest struct {
	subscriberID string
	reply        chan struct{}
}

type stopRequest struct{ reply chan struct{} }
type closeRequest struct{}

type startResult struct {
	generation uint64
	activation chan bool
	err        error
}

type workerStopped struct {
	generation uint64
	err        error
}

type subscriptionMessage struct {
	generation uint64
	capsule    *pb.TimestampedSignedMsgCapsule
}

type Box struct {
	ctx    context.Context
	cancel context.CancelFunc

	logger *zerolog.Logger

	topicID string
	topic   topicBackend

	controlCh chan any
	messageCh chan subscriptionMessage

	done      chan struct{}
	closeOnce sync.Once
	closeErr  error

	workerWG sync.WaitGroup
}

func NewBox(logger *zerolog.Logger, topicID string, topic *pubsub.Topic) (*Box, error) {
	return newBoxWithBackend(logger, topicID, &libp2pTopicAdapter{topic: topic})
}

func newBoxWithBackend(logger *zerolog.Logger, topicID string, topic topicBackend) (*Box, error) {
	subLogger := logger.With().Str("module", "msg-box").Logger()
	ctx, cancel := context.WithCancel(context.Background())
	box := &Box{
		ctx:       ctx,
		cancel:    cancel,
		logger:    &subLogger,
		topicID:   topicID,
		topic:     topic,
		controlCh: make(chan any),
		messageCh: make(chan subscriptionMessage, internalChanBufferSize),
		done:      make(chan struct{}),
	}

	go box.runDispatcher()
	return box, nil
}

type boxDispatcher struct {
	box *Box

	state      subscriptionState
	generation uint64
	pending    map[string]joinRequest
	active     map[string]*Subscriber

	workerCancel context.CancelFunc
}

func (box *Box) runDispatcher() {
	dispatcher := boxDispatcher{
		box:     box,
		state:   subscriptionIdle,
		pending: make(map[string]joinRequest),
		active:  make(map[string]*Subscriber),
	}

	for {
		select {
		case event := <-box.controlCh:
			if dispatcher.handleControl(event) {
				return
			}
			continue
		default:
		}

		select {
		case event := <-box.controlCh:
			if dispatcher.handleControl(event) {
				return
			}
		case message := <-box.messageCh:
			dispatcher.handleMessage(message)
		}
	}
}

func (dispatcher *boxDispatcher) handleControl(event any) bool {
	switch event := event.(type) {
	case joinRequest:
		dispatcher.handleJoin(event)
	case cancelJoinRequest:
		dispatcher.handleCancelJoin(event)
	case leaveRequest:
		dispatcher.handleLeave(event)
	case stopRequest:
		dispatcher.handleStop(event)
	case closeRequest:
		dispatcher.handleClose()
	case startResult:
		dispatcher.handleStartResult(event)
	case workerStopped:
		dispatcher.handleWorkerStopped(event)
	}

	if dispatcher.state != subscriptionClosing || dispatcher.workerCancel != nil {
		return false
	}

	// Terminal events are delivered before workers return. Waiting is safe only
	// after every worker has delivered its terminal event.
	dispatcher.box.workerWG.Wait()
	dispatcher.box.closeErr = dispatcher.box.topic.Close()
	close(dispatcher.box.done)
	return true
}

func (dispatcher *boxDispatcher) handleJoin(request joinRequest) {
	if request.ctx != nil && request.ctx.Err() != nil {
		request.subscriber.stop(request.ctx.Err())
		request.reply <- joinResult{err: request.ctx.Err()}
		return
	}
	if dispatcher.state == subscriptionClosing {
		request.subscriber.stop(ErrBoxClosed)
		request.reply <- joinResult{err: ErrBoxClosed}
		return
	}
	if _, exists := dispatcher.active[request.subscriberID]; exists {
		dispatcher.rejectDuplicate(request)
		return
	}
	if _, exists := dispatcher.pending[request.subscriberID]; exists {
		dispatcher.rejectDuplicate(request)
		return
	}

	switch dispatcher.state {
	case subscriptionIdle:
		dispatcher.pending[request.subscriberID] = request
		dispatcher.startGeneration()
	case subscriptionStarting, subscriptionStopping:
		dispatcher.pending[request.subscriberID] = request
	case subscriptionRunning:
		dispatcher.active[request.subscriberID] = request.subscriber
		request.reply <- joinResult{subscriber: request.subscriber}
	}
}

func (dispatcher *boxDispatcher) handleCancelJoin(request cancelJoinRequest) {
	if pending, ok := dispatcher.pending[request.subscriberID]; ok && pending.subscriber == request.subscriber {
		delete(dispatcher.pending, request.subscriberID)
		pending.subscriber.stop(context.Canceled)
		pending.reply <- joinResult{err: context.Canceled}
		if len(dispatcher.pending) == 0 && dispatcher.state == subscriptionStarting {
			dispatcher.beginStopping()
		}
	}
	if active, ok := dispatcher.active[request.subscriberID]; ok && active == request.subscriber {
		delete(dispatcher.active, request.subscriberID)
		active.stop(context.Canceled)
		if len(dispatcher.active) == 0 && dispatcher.state == subscriptionRunning {
			dispatcher.beginStopping()
		}
	}
}

func (dispatcher *boxDispatcher) rejectDuplicate(request joinRequest) {
	err := fmt.Errorf("%s is already subscribing", request.subscriberID)
	request.subscriber.stop(err)
	request.reply <- joinResult{err: err}
}

func (dispatcher *boxDispatcher) handleLeave(request leaveRequest) {
	if subscriber, exists := dispatcher.active[request.subscriberID]; exists {
		delete(dispatcher.active, request.subscriberID)
		subscriber.stop(nil)
		if len(dispatcher.active) == 0 && dispatcher.state == subscriptionRunning {
			dispatcher.beginStopping()
		}
	}
	request.reply <- struct{}{}
}

func (dispatcher *boxDispatcher) handleStop(request stopRequest) {
	for subscriberID, subscriber := range dispatcher.active {
		delete(dispatcher.active, subscriberID)
		subscriber.stop(nil)
	}

	switch dispatcher.state {
	case subscriptionStarting, subscriptionRunning:
		dispatcher.beginStopping()
	}
	request.reply <- struct{}{}
}

func (dispatcher *boxDispatcher) handleClose() {
	if dispatcher.state == subscriptionClosing {
		return
	}
	dispatcher.state = subscriptionClosing
	dispatcher.box.cancel()

	for subscriberID, request := range dispatcher.pending {
		delete(dispatcher.pending, subscriberID)
		request.subscriber.stop(ErrBoxClosed)
		request.reply <- joinResult{err: ErrBoxClosed}
	}
	for subscriberID, subscriber := range dispatcher.active {
		delete(dispatcher.active, subscriberID)
		subscriber.stop(ErrBoxClosed)
	}
	if dispatcher.workerCancel != nil {
		dispatcher.workerCancel()
	}
}

func (dispatcher *boxDispatcher) handleStartResult(result startResult) {
	if result.generation != dispatcher.generation {
		if result.err == nil {
			result.activation <- false
		}
		return
	}

	if result.err != nil {
		dispatcher.workerCancel = nil
		switch dispatcher.state {
		case subscriptionStarting:
			dispatcher.failPending(result.err)
			dispatcher.state = subscriptionIdle
		case subscriptionStopping:
			dispatcher.restartOrIdle()
		case subscriptionClosing:
			// A failed start result is this worker's terminal event.
		}
		return
	}

	switch dispatcher.state {
	case subscriptionStarting:
		for subscriberID, request := range dispatcher.pending {
			if request.ctx != nil && request.ctx.Err() != nil {
				delete(dispatcher.pending, subscriberID)
				request.subscriber.stop(request.ctx.Err())
				request.reply <- joinResult{err: request.ctx.Err()}
			}
		}
		if len(dispatcher.pending) == 0 {
			dispatcher.beginStopping()
			result.activation <- false
			return
		}
		dispatcher.state = subscriptionRunning
		result.activation <- true
		for subscriberID, request := range dispatcher.pending {
			delete(dispatcher.pending, subscriberID)
			dispatcher.active[subscriberID] = request.subscriber
			request.reply <- joinResult{subscriber: request.subscriber}
		}
	case subscriptionStopping, subscriptionClosing:
		result.activation <- false
	default:
		result.activation <- false
	}
}

func (dispatcher *boxDispatcher) handleWorkerStopped(stopped workerStopped) {
	if stopped.generation != dispatcher.generation {
		return
	}
	dispatcher.workerCancel = nil

	switch dispatcher.state {
	case subscriptionRunning:
		if stopped.err != nil {
			dispatcher.box.logger.Err(stopped.err).
				Str("topic-id", dispatcher.box.topicID).
				Msg("subscription stopped unexpectedly")
		}
		for subscriberID, subscriber := range dispatcher.active {
			delete(dispatcher.active, subscriberID)
			subscriber.stop(stopped.err)
		}
		dispatcher.state = subscriptionIdle
	case subscriptionStopping:
		if stopped.err != nil {
			dispatcher.box.logger.Err(stopped.err).
				Str("topic-id", dispatcher.box.topicID).
				Msg("subscription stopped with error")
		}
		dispatcher.restartOrIdle()
	case subscriptionClosing:
		// Errors do not prevent shutdown once the worker is terminal.
	}
}

func (dispatcher *boxDispatcher) handleMessage(message subscriptionMessage) {
	if dispatcher.state != subscriptionRunning || message.generation != dispatcher.generation {
		return
	}
	for subscriberID, subscriber := range dispatcher.active {
		select {
		case subscriber.messages <- message.capsule:
		default:
			delete(dispatcher.active, subscriberID)
			subscriber.stop(ErrSlowSubscriber)
			dispatcher.box.logger.Warn().
				Str("topic-id", dispatcher.box.topicID).
				Str("subscriber-id", subscriberID).
				Int("queue-length", len(subscriber.messages)).
				Int("queue-capacity", cap(subscriber.messages)).
				Msg("removed slow subscriber")
		}
	}
	if len(dispatcher.active) == 0 {
		dispatcher.beginStopping()
	}
}

func (dispatcher *boxDispatcher) startGeneration() {
	dispatcher.generation++
	dispatcher.state = subscriptionStarting
	workerCtx, cancel := context.WithCancel(context.Background())
	dispatcher.workerCancel = cancel
	dispatcher.box.startWorker(workerCtx, dispatcher.generation)
}

func (dispatcher *boxDispatcher) beginStopping() {
	dispatcher.state = subscriptionStopping
	if dispatcher.workerCancel != nil {
		dispatcher.workerCancel()
	}
}

func (dispatcher *boxDispatcher) restartOrIdle() {
	if len(dispatcher.pending) == 0 {
		dispatcher.state = subscriptionIdle
		return
	}
	dispatcher.startGeneration()
}

func (dispatcher *boxDispatcher) failPending(err error) {
	for subscriberID, request := range dispatcher.pending {
		delete(dispatcher.pending, subscriberID)
		request.subscriber.stop(err)
		request.reply <- joinResult{err: err}
	}
}

func (box *Box) startWorker(ctx context.Context, generation uint64) {
	box.workerWG.Add(1)
	go func() {
		defer box.workerWG.Done()

		subscription, err := box.topic.Subscribe()
		activation := make(chan bool, 1)
		box.controlCh <- startResult{
			generation: generation,
			activation: activation,
			err:        err,
		}
		if err != nil {
			return
		}

		if activate := <-activation; !activate {
			subscription.Cancel()
			box.controlCh <- workerStopped{generation: generation}
			return
		}

		box.logger.Info().Str("topic-id", box.topicID).Msg("started subscription")
		var terminalErr error
		for {
			data, nextErr := subscription.Next(ctx)
			if nextErr != nil {
				if ctx.Err() == nil &&
					!errors.Is(nextErr, context.Canceled) &&
					!errors.Is(nextErr, pubsub.ErrSubscriptionCancelled) {
					terminalErr = nextErr
				}
				break
			}

			capsule := &pb.TimestampedSignedMsgCapsule{}
			if unmarshalErr := proto.Unmarshal(data, capsule); unmarshalErr != nil {
				box.logger.Err(unmarshalErr).
					Str("topic-id", box.topicID).
					Msg("failed to decode subscription message")
				continue
			}

			select {
			case box.messageCh <- subscriptionMessage{generation: generation, capsule: capsule}:
			case <-ctx.Done():
				terminalErr = nil
			}
			if ctx.Err() != nil {
				break
			}
		}

		subscription.Cancel()
		box.logger.Info().Str("topic-id", box.topicID).Msg("stopped subscription")
		box.controlCh <- workerStopped{generation: generation, err: terminalErr}
	}()
}

// StopSub stops current subscribers and cancels the current subscription.
// Joins already pending during a stop remain pending for the next subscription.
// It returns after the stop request is handled, before the worker necessarily exits.
func (box *Box) StopSub() {
	reply := make(chan struct{}, 1)
	select {
	case <-box.ctx.Done():
		return
	case <-box.done:
		return
	case box.controlCh <- stopRequest{reply: reply}:
	}
	select {
	case <-box.ctx.Done():
	case <-box.done:
	case <-reply:
	}
}

func (box *Box) Close() error {
	box.closeOnce.Do(func() {
		select {
		case <-box.done:
		case box.controlCh <- closeRequest{}:
		}
	})
	<-box.done
	return box.closeErr
}

func (box *Box) Publish(signedMsgCapsule *pb.SignedMsgCapsule) error {
	timestamped := &pb.TimestampedSignedMsgCapsule{
		Timestamp:        time.Now().UnixNano(),
		SignedMsgCapsule: signedMsgCapsule,
	}
	data, err := proto.Marshal(timestamped)
	if err != nil {
		return err
	}
	return box.topic.Publish(box.ctx, data)
}

func (box *Box) JoinSub(subscriberID string) (*Subscriber, error) {
	return box.JoinSubContext(context.Background(), subscriberID)
}

func (box *Box) JoinSubContext(ctx context.Context, subscriberID string) (*Subscriber, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	subscriber := newSubscriber(externalChanBufferSize)
	reply := make(chan joinResult, 1)
	request := joinRequest{subscriberID: subscriberID, subscriber: subscriber, reply: reply, ctx: ctx}

	select {
	case <-ctx.Done():
		subscriber.stop(ctx.Err())
		return nil, ctx.Err()
	case <-box.ctx.Done():
		subscriber.stop(ErrBoxClosed)
		return nil, ErrBoxClosed
	case <-box.done:
		subscriber.stop(ErrBoxClosed)
		return nil, ErrBoxClosed
	case box.controlCh <- request:
	}

	select {
	case <-ctx.Done():
		subscriber.stop(ctx.Err())
		go func() {
			select {
			case box.controlCh <- cancelJoinRequest{subscriberID: subscriberID, subscriber: subscriber}:
			case <-box.done:
			}
		}()
		return nil, ctx.Err()
	case <-box.ctx.Done():
		subscriber.stop(ErrBoxClosed)
		return nil, ErrBoxClosed
	case <-box.done:
		subscriber.stop(ErrBoxClosed)
		return nil, ErrBoxClosed
	case result := <-reply:
		if result.err != nil {
			return nil, result.err
		}
		if err := ctx.Err(); err != nil {
			go func() {
				select {
				case box.controlCh <- cancelJoinRequest{subscriberID: subscriberID, subscriber: subscriber}:
				case <-box.done:
				}
			}()
			return nil, err
		}
		return result.subscriber, nil
	}
}

func (box *Box) LeaveSub(subscriberID string) error {
	reply := make(chan struct{}, 1)
	request := leaveRequest{subscriberID: subscriberID, reply: reply}
	select {
	case <-box.ctx.Done():
		return nil
	case <-box.done:
		return nil
	case box.controlCh <- request:
	}
	select {
	case <-box.ctx.Done():
		return nil
	case <-box.done:
		return nil
	case <-reply:
		return nil
	}
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
