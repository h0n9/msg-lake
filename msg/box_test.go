package msg

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	pb "github.com/h0n9/msg-lake/proto"
)

const testTimeout = time.Second

type fakeSubscribeOutcome struct {
	subscription *fakeSubscription
	err          error
}

type fakeTopic struct {
	subscribeEntered chan struct{}
	outcomes         chan fakeSubscribeOutcome
	teardown         chan struct{}
	teardownOnce     sync.Once

	mu            sync.Mutex
	subscriptions []*fakeSubscription

	subscribeCalls atomic.Int32
	closeCalls     atomic.Int32
	closeErr       error
	closeEntered   chan struct{}
	closeRelease   chan struct{}
}

func newFakeTopic() *fakeTopic {
	return &fakeTopic{
		subscribeEntered: make(chan struct{}, 32),
		outcomes:         make(chan fakeSubscribeOutcome, 32),
		teardown:         make(chan struct{}),
	}
}

func (topic *fakeTopic) Subscribe() (subscriptionBackend, error) {
	topic.subscribeCalls.Add(1)
	topic.subscribeEntered <- struct{}{}
	var outcome fakeSubscribeOutcome
	select {
	case outcome = <-topic.outcomes:
	case <-topic.teardown:
		return nil, errors.New("fake topic torn down")
	}
	if outcome.err != nil {
		return nil, outcome.err
	}
	topic.mu.Lock()
	topic.subscriptions = append(topic.subscriptions, outcome.subscription)
	topic.mu.Unlock()
	return outcome.subscription, nil
}

func (topic *fakeTopic) Publish(context.Context, []byte) error { return nil }

func (topic *fakeTopic) Close() error {
	topic.closeCalls.Add(1)
	if topic.closeEntered != nil {
		close(topic.closeEntered)
	}
	if topic.closeRelease != nil {
		<-topic.closeRelease
	}
	return topic.closeErr
}

func (topic *fakeTopic) release() {
	topic.teardownOnce.Do(func() { close(topic.teardown) })
	topic.mu.Lock()
	defer topic.mu.Unlock()
	for _, subscription := range topic.subscriptions {
		subscription.releaseCancel()
	}
}

type fakeNextResult struct {
	data []byte
	err  error
}

type fakeSubscription struct {
	next          chan fakeNextResult
	nextReturned  chan struct{}
	cancelEntered chan struct{}
	cancelRelease chan struct{}
	canceled      chan struct{}
	nextOnce      sync.Once
	enterOnce     sync.Once
	releaseOnce   sync.Once
	cancelOnce    sync.Once
	cancelCalls   atomic.Int32
}

func newFakeSubscription() *fakeSubscription {
	subscription := newBlockingCancelSubscription()
	subscription.releaseCancel()
	return subscription

}

func newBlockingCancelSubscription() *fakeSubscription {
	return &fakeSubscription{
		next:          make(chan fakeNextResult, 32),
		cancelEntered: make(chan struct{}),
		cancelRelease: make(chan struct{}),
		canceled:      make(chan struct{}),
	}
}

func (subscription *fakeSubscription) Next(ctx context.Context) ([]byte, error) {
	select {
	case result := <-subscription.next:
		if subscription.nextReturned != nil {
			subscription.nextOnce.Do(func() { close(subscription.nextReturned) })
		}
		return result.data, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestCloseCancelsWorkerBlockedOnFullMessageChannel(t *testing.T) {
	logger := zerolog.Nop()
	topic := newFakeTopic()
	ctx, cancel := context.WithCancel(context.Background())
	box := &Box{
		ctx:       ctx,
		cancel:    cancel,
		logger:    &logger,
		topicID:   "full-message-channel",
		topic:     topic,
		controlCh: make(chan any),
		messageCh: make(chan subscriptionMessage, 1),
		done:      make(chan struct{}),
	}
	dispatcher := boxDispatcher{
		box:     box,
		state:   subscriptionIdle,
		pending: make(map[string]joinRequest),
		active:  make(map[string]*Subscriber),
	}
	box.messageCh <- subscriptionMessage{generation: 999}

	defer cleanupManualDispatcher(box, topic, cancel, &dispatcher)

	subscription := newFakeSubscription()
	subscription.nextReturned = make(chan struct{})
	topic.outcomes <- fakeSubscribeOutcome{subscription: subscription}
	joinReply := make(chan joinResult, 1)
	dispatcher.handleJoin(joinRequest{
		subscriberID: "subscriber",
		subscriber:   newSubscriber(externalChanBufferSize),
		reply:        joinReply,
	})

	start := waitControlEvent(t, box.controlCh, "subscription start result")
	if done := dispatcher.handleControl(start); done {
		t.Fatal("dispatcher closed while starting subscription")
	}
	if result := waitJoinResult(t, joinReply); result.err != nil {
		t.Fatalf("JoinSub error = %v", result.err)
	}

	subscription.next <- fakeNextResult{data: marshalTimestampedMessage(t, "blocked")}
	waitSignal(t, subscription.nextReturned, "Next return")
	if got := len(box.messageCh); got != 1 {
		t.Fatalf("message channel length = %d, want 1", got)
	}

	closeResult := make(chan error, 1)
	go func() { closeResult <- box.Close() }()
	closeEvent := waitControlEvent(t, box.controlCh, "Close request")
	if done := dispatcher.handleControl(closeEvent); done {
		t.Fatal("dispatcher closed before worker terminal event")
	}

	stoppedEvent := waitControlEvent(t, box.controlCh, "worker stopped event")
	stopped, ok := stoppedEvent.(workerStopped)
	if !ok {
		t.Fatalf("control event = %T, want workerStopped", stoppedEvent)
	}
	if stopped.err != nil {
		t.Fatalf("worker stopped error = %v, want nil", stopped.err)
	}
	if got := len(box.messageCh); got != 1 {
		t.Fatalf("message channel length after cancellation = %d, want 1", got)
	}
	if done := dispatcher.handleControl(stoppedEvent); !done {
		t.Fatal("dispatcher did not close after worker terminal event")
	}
	if err := waitError(t, closeResult); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got := subscription.cancelCalls.Load(); got != 1 {
		t.Fatalf("Subscription Cancel calls = %d, want 1", got)
	}
	if got := topic.closeCalls.Load(); got != 1 {
		t.Fatalf("Topic Close calls = %d, want 1", got)
	}
}

func TestStaleLifecycleEventsDoNotMutateCurrentGeneration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*boxDispatcher) chan bool
	}{
		{"start success", func(d *boxDispatcher) chan bool {
			activation := make(chan bool, 1)
			d.handleStartResult(startResult{generation: 1, activation: activation})
			return activation
		}},
		{"start failure", func(d *boxDispatcher) chan bool {
			d.handleStartResult(startResult{generation: 1, err: errors.New("stale start")})
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pending := joinRequest{subscriberID: "pending", subscriber: newSubscriber(externalChanBufferSize), reply: make(chan joinResult, 1)}
			d := &boxDispatcher{state: subscriptionStarting, generation: 2,
				pending: map[string]joinRequest{"pending": pending}, workerCancel: func() { t.Error("canceled current worker") }}
			activation := tc.apply(d)
			if d.state != subscriptionStarting || d.generation != 2 || d.workerCancel == nil || len(d.pending) != 1 {
				t.Fatal("stale start changed the current generation")
			}
			assertSubscriberRunning(t, pending.subscriber)
			select {
			case <-pending.reply:
				t.Fatal("stale start replied to pending Join")
			default:
			}
			if activation != nil {
				select {
				case activate := <-activation:
					if activate {
						t.Fatal("stale start was activated")
					}
				default:
					t.Fatal("stale start received no activation decision")
				}
			}
		})
	}

	t.Run("stopped", func(t *testing.T) {
		active := newSubscriber(externalChanBufferSize)
		d := &boxDispatcher{state: subscriptionRunning, generation: 2,
			active: map[string]*Subscriber{"active": active}, workerCancel: func() { t.Error("canceled current worker") }}
		d.handleWorkerStopped(workerStopped{generation: 1, err: errors.New("stale stop")})
		if d.state != subscriptionRunning || d.generation != 2 || d.workerCancel == nil || d.active["active"] != active {
			t.Fatal("stale stop changed the current generation")
		}
		assertSubscriberRunning(t, active)
	})
}

func (subscription *fakeSubscription) Cancel() {
	subscription.cancelCalls.Add(1)
	subscription.enterOnce.Do(func() { close(subscription.cancelEntered) })
	<-subscription.cancelRelease
	subscription.cancelOnce.Do(func() { close(subscription.canceled) })
}

func (subscription *fakeSubscription) releaseCancel() {
	subscription.releaseOnce.Do(func() { close(subscription.cancelRelease) })
}

func TestDefaultExternalChanBufferSize(t *testing.T) {
	if DefaultExternalChanBufferSize != 256 {
		t.Fatalf("DefaultExternalChanBufferSize = %d, want 256", DefaultExternalChanBufferSize)
	}
}

func TestLoadExternalChanBufferSize(t *testing.T) {
	t.Run("override", func(t *testing.T) {
		t.Setenv("EXTERNAL_CHAN_BUFFER_SIZE", "512")
		if got := loadExternalChanBufferSize(); got != 512 {
			t.Fatalf("loadExternalChanBufferSize() = %d, want 512", got)
		}
	})

	for _, value := range []string{"0", "-1"} {
		t.Run("reject "+value, func(t *testing.T) {
			t.Setenv("EXTERNAL_CHAN_BUFFER_SIZE", value)
			defer func() {
				if recover() == nil {
					t.Fatalf("loadExternalChanBufferSize() did not panic for %q", value)
				}
			}()
			loadExternalChanBufferSize()
		})
	}
}

func TestJoinWaitsForSubscriptionAndSharesStart(t *testing.T) {
	box, topic := newFakeBox(t)
	firstResult := asyncJoin(box, "first")
	waitSignal(t, topic.subscribeEntered, "subscription start")
	secondResult := enqueueJoinDuringStart(t, box, "second")
	assertNoJoinResult(t, firstResult)
	assertNoJoinResult(t, secondResult)
	if got := topic.subscribeCalls.Load(); got != 1 {
		t.Fatalf("Subscribe calls = %d, want 1", got)
	}

	subscription := newFakeSubscription()
	topic.outcomes <- fakeSubscribeOutcome{subscription: subscription}
	for _, result := range []<-chan joinResult{firstResult, secondResult} {
		if got := waitJoinResult(t, result); got.err != nil || got.subscriber == nil {
			t.Fatalf("JoinSub result = (%v, %v), want subscriber and nil error", got.subscriber, got.err)
		}
	}
	if got := topic.subscribeCalls.Load(); got != 1 {
		t.Fatalf("Subscribe calls = %d, want 1", got)
	}
}

func TestStartFailureFailsPendingJoins(t *testing.T) {
	box, topic := newFakeBox(t)
	wantErr := errors.New("subscribe failed")
	firstResult := asyncJoin(box, "first")
	waitSignal(t, topic.subscribeEntered, "subscription start")
	secondResult := enqueueJoinDuringStart(t, box, "second")
	topic.outcomes <- fakeSubscribeOutcome{err: wantErr}
	for _, result := range []<-chan joinResult{firstResult, secondResult} {
		if got := waitJoinResult(t, result); !errors.Is(got.err, wantErr) {
			t.Fatalf("JoinSub error = %v, want %v", got.err, wantErr)
		}
	}

	subscription := newFakeSubscription()
	topic.outcomes <- fakeSubscribeOutcome{subscription: subscription}
	joined := waitJoinResult(t, asyncJoin(box, "second"))
	if joined.err != nil {
		t.Fatalf("JoinSub after failure error = %v", joined.err)
	}
}

func TestBoxFanOutPreservesOrder(t *testing.T) {
	box, _, subscription, first := newRunningBox(t, "first")
	second := joinTestSubscriber(t, box, "second")

	for _, data := range []string{"one", "two", "three"} {
		subscription.next <- fakeNextResult{data: marshalTimestampedMessage(t, data)}
	}
	for _, subscriber := range []*Subscriber{first, second} {
		for _, want := range []string{"one", "two", "three"} {
			message := waitMessage(t, subscriber)
			if got := string(message.GetSignedMsgCapsule().GetMsgCapsule().GetData()); got != want {
				t.Fatalf("message data = %q, want %q", got, want)
			}
		}
	}
}

func TestBoxFanOutRemovesOnlySlowSubscriber(t *testing.T) {
	box, _, subscription, slow := newRunningBox(t, "slow")
	healthy := joinTestSubscriber(t, box, "healthy")

	for range cap(slow.messages) {
		slow.messages <- testTimestampedMessage("queued")
	}
	subscription.next <- fakeNextResult{data: marshalTimestampedMessage(t, "first")}

	waitDone(t, slow, ErrSlowSubscriber)
	if got := string(waitMessage(t, healthy).GetSignedMsgCapsule().GetMsgCapsule().GetData()); got != "first" {
		t.Fatalf("healthy message = %q, want first", got)
	}
	assertOperationCompletes(t, func() error { return box.LeaveSub("slow") })
	assertOperationCompletes(t, func() error { return box.LeaveSub("slow") })
}

func TestLeaveLastSubscriberStopsWorkerAndAllowsRestart(t *testing.T) {
	box, topic, firstSub, _ := newRunningBox(t, "first")
	if err := box.LeaveSub("first"); err != nil {
		t.Fatalf("LeaveSub() error = %v", err)
	}
	waitSignal(t, firstSub.canceled, "first subscription cancellation")

	secondSub := newFakeSubscription()
	topic.outcomes <- fakeSubscribeOutcome{subscription: secondSub}
	second := joinTestSubscriber(t, box, "second")
	if second == nil {
		t.Fatal("second JoinSub returned nil")
	}
	if got := topic.subscribeCalls.Load(); got != 2 {
		t.Fatalf("Subscribe calls = %d, want 2", got)
	}
}

func TestStaleGenerationMessageIsDiscarded(t *testing.T) {
	box, topic, firstSub, _ := newRunningBox(t, "first")
	if err := box.LeaveSub("first"); err != nil {
		t.Fatalf("LeaveSub() error = %v", err)
	}
	waitSignal(t, firstSub.canceled, "first subscription cancellation")

	secondSub := newFakeSubscription()
	topic.outcomes <- fakeSubscribeOutcome{subscription: secondSub}
	second := joinTestSubscriber(t, box, "second")
	box.messageCh <- subscriptionMessage{
		generation: 1,
		capsule:    testTimestampedMessage("stale"),
	}
	select {
	case message := <-second.Messages():
		t.Fatalf("received stale message: %v", message)
	default:
	}

	secondSub.next <- fakeNextResult{data: marshalTimestampedMessage(t, "current")}
	if got := string(waitMessage(t, second).GetSignedMsgCapsule().GetMsgCapsule().GetData()); got != "current" {
		t.Fatalf("message data = %q, want current", got)
	}
}

func TestLeaveDoesNotCancelPendingJoinWhileStopping(t *testing.T) {
	box, topic := newFakeBox(t)
	firstSub := newBlockingCancelSubscription()
	topic.outcomes <- fakeSubscribeOutcome{subscription: firstSub}
	_ = joinTestSubscriber(t, box, "first")
	if err := box.LeaveSub("first"); err != nil {
		t.Fatalf("LeaveSub(first) error = %v", err)
	}
	waitSignal(t, firstSub.cancelEntered, "first subscription Cancel entry")

	pendingSubscriber := newSubscriber(externalChanBufferSize)
	pendingReply := make(chan joinResult, 1)
	box.controlCh <- joinRequest{
		subscriberID: "pending",
		subscriber:   pendingSubscriber,
		reply:        pendingReply,
	}
	if err := box.LeaveSub("pending"); err != nil {
		t.Fatalf("LeaveSub(pending) error = %v", err)
	}

	replacement := newFakeSubscription()
	topic.outcomes <- fakeSubscribeOutcome{subscription: replacement}
	firstSub.releaseCancel()
	result := waitJoinResult(t, pendingReply)
	if result.err != nil || result.subscriber != pendingSubscriber {
		t.Fatalf("pending Join result = (%v, %v), want original subscriber and nil error", result.subscriber, result.err)
	}
}

func TestStopSubStopsSubscribers(t *testing.T) {
	box, _, subscription, subscriber := newRunningBox(t, "subscriber")
	box.StopSub()
	waitDone(t, subscriber, nil)
	waitSignal(t, subscription.canceled, "subscription cancellation")
}

func TestStopSubDuringStartPreservesPendingJoin(t *testing.T) {
	for _, firstOutcome := range []struct {
		name string
		err  error
	}{
		{name: "late success"},
		{name: "late failure", err: errors.New("first start failed")},
	} {
		t.Run(firstOutcome.name, func(t *testing.T) {
			box, topic := newFakeBox(t)
			join := asyncJoin(box, "subscriber")
			waitSignal(t, topic.subscribeEntered, "first subscription start")
			box.StopSub()

			firstSubscription := newFakeSubscription()
			topic.outcomes <- fakeSubscribeOutcome{
				subscription: firstSubscription,
				err:          firstOutcome.err,
			}
			if firstOutcome.err == nil {
				waitSignal(t, firstSubscription.canceled, "first subscription cancellation")
			}

			waitSignal(t, topic.subscribeEntered, "replacement subscription start")
			replacement := newFakeSubscription()
			topic.outcomes <- fakeSubscribeOutcome{subscription: replacement}
			if result := waitJoinResult(t, join); result.err != nil || result.subscriber == nil {
				t.Fatalf("pending JoinSub result = (%v, %v), want subscriber and nil error", result.subscriber, result.err)
			}
			if got := topic.subscribeCalls.Load(); got != 2 {
				t.Fatalf("Subscribe calls = %d, want 2", got)
			}
		})
	}
}

func TestUnexpectedNextErrorStopsSubscribersWithoutRestart(t *testing.T) {
	_, topic, subscription, subscriber := newRunningBox(t, "subscriber")
	wantErr := errors.New("read failed")
	subscription.next <- fakeNextResult{err: wantErr}
	waitDone(t, subscriber, wantErr)
	waitSignal(t, subscription.canceled, "subscription cancellation")
	if got := topic.subscribeCalls.Load(); got != 1 {
		t.Fatalf("Subscribe calls = %d, want 1", got)
	}
}

func TestMalformedMessageDoesNotStopWorker(t *testing.T) {
	_, _, subscription, subscriber := newRunningBox(t, "subscriber")
	subscription.next <- fakeNextResult{data: []byte("not protobuf")}
	subscription.next <- fakeNextResult{data: marshalTimestampedMessage(t, "valid")}
	if got := string(waitMessage(t, subscriber).GetSignedMsgCapsule().GetMsgCapsule().GetData()); got != "valid" {
		t.Fatalf("message data = %q, want valid", got)
	}
}

func TestCloseStopsSubscriberBeforeTopicClose(t *testing.T) {
	box, topic := newFakeBox(t)
	subscription := newBlockingCancelSubscription()
	topic.outcomes <- fakeSubscribeOutcome{subscription: subscription}
	subscriber := joinTestSubscriber(t, box, "subscriber")
	wantErr := errors.New("topic close failed")
	topic.closeErr = wantErr

	closeResult := make(chan error, 2)
	go func() { closeResult <- box.Close() }()
	go func() { closeResult <- box.Close() }()

	waitDone(t, subscriber, ErrBoxClosed)
	waitSignal(t, subscription.cancelEntered, "subscription Cancel entry")
	if got := topic.closeCalls.Load(); got != 0 {
		t.Fatalf("Topic Close calls before Cancel returned = %d, want 0", got)
	}
	joinResult := asyncJoin(box, "late")
	if result := waitJoinResult(t, joinResult); !errors.Is(result.err, ErrBoxClosed) {
		t.Fatalf("JoinSub during Close error = %v, want %v", result.err, ErrBoxClosed)
	}
	assertOperationCompletes(t, func() error { return box.LeaveSub("late") })
	stopDone := make(chan struct{}, 1)
	go func() {
		box.StopSub()
		stopDone <- struct{}{}
	}()
	waitSignal(t, stopDone, "StopSub during Close")
	select {
	case err := <-closeResult:
		t.Fatalf("Close() returned before Cancel completed: %v", err)
	default:
	}
	subscription.releaseCancel()
	waitSignal(t, subscription.canceled, "subscription cancellation")
	for range 2 {
		if err := waitError(t, closeResult); !errors.Is(err, wantErr) {
			t.Fatalf("Close() error = %v, want %v", err, wantErr)
		}
	}
	if got := topic.closeCalls.Load(); got != 1 {
		t.Fatalf("Topic Close calls = %d, want 1", got)
	}
	if got := subscription.cancelCalls.Load(); got != 1 {
		t.Fatalf("Subscription Cancel calls = %d, want 1", got)
	}
}

func TestCloseWhileSubscribeIsDelayed(t *testing.T) {
	box, topic := newFakeBox(t)
	join := asyncJoin(box, "subscriber")
	waitSignal(t, topic.subscribeEntered, "subscription start")
	closeResult := make(chan error, 1)
	go func() { closeResult <- box.Close() }()
	select {
	case <-box.ctx.Done():
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for Close to linearize")
	}

	subscription := newFakeSubscription()
	topic.outcomes <- fakeSubscribeOutcome{subscription: subscription}
	waitSignal(t, subscription.canceled, "late subscription cancellation")
	if err := waitError(t, closeResult); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if result := waitJoinResult(t, join); !errors.Is(result.err, ErrBoxClosed) {
		t.Fatalf("JoinSub error = %v, want %v", result.err, ErrBoxClosed)
	}
}

func TestCloseWhileSubscribeFails(t *testing.T) {
	box, topic := newFakeBox(t)
	join := asyncJoin(box, "subscriber")
	waitSignal(t, topic.subscribeEntered, "subscription start")
	closeResult := make(chan error, 1)
	go func() { closeResult <- box.Close() }()
	select {
	case <-box.ctx.Done():
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for Close to linearize")
	}

	topic.outcomes <- fakeSubscribeOutcome{err: errors.New("subscribe failed")}
	if err := waitError(t, closeResult); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if result := waitJoinResult(t, join); !errors.Is(result.err, ErrBoxClosed) {
		t.Fatalf("JoinSub error = %v, want %v", result.err, ErrBoxClosed)
	}
	if got := topic.closeCalls.Load(); got != 1 {
		t.Fatalf("Topic Close calls = %d, want 1", got)
	}
}

func TestAPIsReturnWhileTopicCloseIsBlocked(t *testing.T) {
	box, topic := newFakeBox(t)
	topic.closeEntered = make(chan struct{})
	topic.closeRelease = make(chan struct{})
	var releaseOnce sync.Once
	releaseClose := func() { releaseOnce.Do(func() { close(topic.closeRelease) }) }
	defer releaseClose()

	closeResult := make(chan error, 1)
	go func() { closeResult <- box.Close() }()
	waitSignal(t, topic.closeEntered, "Topic.Close entry")
	if result := waitJoinResult(t, asyncJoin(box, "late")); !errors.Is(result.err, ErrBoxClosed) {
		t.Fatalf("JoinSub error = %v, want %v", result.err, ErrBoxClosed)
	}
	assertOperationCompletes(t, func() error { return box.LeaveSub("absent") })
	stopDone := make(chan struct{}, 1)
	go func() { box.StopSub(); stopDone <- struct{}{} }()
	waitSignal(t, stopDone, "StopSub during Topic.Close")
	select {
	case <-closeResult:
		t.Fatal("Box.Close returned before Topic.Close finished")
	default:
	}
	releaseClose()
	if err := waitError(t, closeResult); err != nil {
		t.Fatalf("Box.Close error = %v", err)
	}
}

func TestJoinSubAfterClose(t *testing.T) {
	box, _ := newFakeBox(t)
	if err := box.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := box.JoinSub("late"); !errors.Is(err, ErrBoxClosed) {
		t.Fatalf("JoinSub() error = %v, want %v", err, ErrBoxClosed)
	}
	assertOperationCompletes(t, func() error { return box.LeaveSub("late") })
	box.StopSub()
}

func newFakeBox(t *testing.T) (*Box, *fakeTopic) {
	t.Helper()
	logger := zerolog.Nop()
	topic := newFakeTopic()
	box, err := newBoxWithBackend(&logger, "test-topic", topic)
	if err != nil {
		t.Fatalf("newBoxWithBackend() error = %v", err)
	}
	t.Cleanup(func() {
		topic.release()
		closeResult := make(chan error, 1)
		go func() { closeResult <- box.Close() }()
		select {
		case err := <-closeResult:
			if err != nil && topic.closeErr == nil {
				t.Errorf("Box.Close() cleanup error = %v", err)
			}
		case <-time.After(testTimeout):
			t.Error("timed out closing Box during cleanup")
		}
		if got := topic.closeCalls.Load(); got != 1 {
			t.Errorf("Topic Close calls = %d, want 1", got)
		}
	})
	return box, topic
}

func newRunningBox(t *testing.T, subscriberID string) (*Box, *fakeTopic, *fakeSubscription, *Subscriber) {
	t.Helper()
	box, topic := newFakeBox(t)
	subscription := newFakeSubscription()
	topic.outcomes <- fakeSubscribeOutcome{subscription: subscription}
	subscriber := joinTestSubscriber(t, box, subscriberID)
	return box, topic, subscription, subscriber
}

func joinTestSubscriber(t *testing.T, box *Box, subscriberID string) *Subscriber {
	t.Helper()
	subscriber, err := box.JoinSub(subscriberID)
	if err != nil {
		t.Fatalf("JoinSub(%q) error = %v", subscriberID, err)
	}
	return subscriber
}

func asyncJoin(box *Box, subscriberID string) <-chan joinResult {
	result := make(chan joinResult, 1)
	go func() {
		subscriber, err := box.JoinSub(subscriberID)
		result <- joinResult{subscriber: subscriber, err: err}
	}()
	return result
}

// A later control request is acknowledged only after the join is handled.
func enqueueJoinDuringStart(t *testing.T, box *Box, subscriberID string) <-chan joinResult {
	t.Helper()
	result := make(chan joinResult, 1)
	request := joinRequest{subscriberID: subscriberID, subscriber: newSubscriber(externalChanBufferSize), reply: result}
	select {
	case box.controlCh <- request:
	case <-time.After(testTimeout):
		t.Fatal("timed out enqueuing Join during start")
	}
	barrier := make(chan struct{}, 1)
	select {
	case box.controlCh <- leaveRequest{subscriberID: "absent", reply: barrier}:
	case <-time.After(testTimeout):
		t.Fatal("timed out enqueuing control barrier")
	}
	waitSignal(t, barrier, "control barrier")
	return result
}

func waitJoinResult(t *testing.T, result <-chan joinResult) joinResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for JoinSub")
		return joinResult{}
	}
}

func assertNoJoinResult(t *testing.T, result <-chan joinResult) {
	t.Helper()
	select {
	case got := <-result:
		t.Fatalf("JoinSub completed before subscription start: %v", got.err)
	default:
	}
}

func waitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(testTimeout):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitMessage(t *testing.T, subscriber *Subscriber) *pb.TimestampedSignedMsgCapsule {
	t.Helper()
	select {
	case message := <-subscriber.Messages():
		return message
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for message")
		return nil
	}
}

func waitDone(t *testing.T, subscriber *Subscriber, wantErr error) {
	t.Helper()
	select {
	case <-subscriber.Done():
		if !errors.Is(subscriber.Err(), wantErr) {
			t.Fatalf("subscriber error = %v, want %v", subscriber.Err(), wantErr)
		}
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for subscriber to stop")
	}
}

func waitError(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(testTimeout):
		t.Fatal("timed out waiting for operation")
		return nil
	}
}

func assertOperationCompletes(t *testing.T, operation func() error) {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- operation() }()
	if err := waitError(t, result); err != nil {
		t.Fatalf("operation error = %v", err)
	}
}

func marshalTimestampedMessage(t *testing.T, data string) []byte {
	t.Helper()
	encoded, err := proto.Marshal(testTimestampedMessage(data))
	if err != nil {
		t.Fatalf("proto.Marshal() error = %v", err)
	}
	return encoded
}

func testTimestampedMessage(data string) *pb.TimestampedSignedMsgCapsule {
	return &pb.TimestampedSignedMsgCapsule{
		SignedMsgCapsule: &pb.SignedMsgCapsule{
			MsgCapsule: &pb.MsgCapsule{Data: []byte(data)},
		},
	}
}

func waitControlEvent(t *testing.T, controlCh <-chan any, description string) any {
	t.Helper()
	select {
	case event := <-controlCh:
		return event
	case <-time.After(testTimeout):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}

func cleanupManualDispatcher(
	box *Box,
	topic *fakeTopic,
	cancel context.CancelFunc,
	dispatcher *boxDispatcher,
) {
	topic.release()
	cancel()
	select {
	case <-box.done:
		return
	default:
	}

	go func() { _ = box.Close() }()
	timer := time.NewTimer(testTimeout)
	defer timer.Stop()
	for {
		select {
		case <-box.done:
			return
		case event := <-box.controlCh:
			if dispatcher.handleControl(event) {
				return
			}
		case <-box.messageCh:
		case <-timer.C:
			return
		}
	}
}

func assertSubscriberRunning(t *testing.T, subscriber *Subscriber) {
	t.Helper()
	select {
	case <-subscriber.Done():
		t.Fatalf("subscriber stopped with error %v", subscriber.Err())
	default:
	}
}
