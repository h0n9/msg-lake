package msg

import (
	"context"
	"errors"
	"testing"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/rs/zerolog"

	pb "github.com/h0n9/msg-lake/proto"
)

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

func TestBoxFanOutPreservesOrder(t *testing.T) {
	box := newTestBox(t)
	first := joinTestSubscriber(t, box, "first")
	second := joinTestSubscriber(t, box, "second")

	messages := []*pb.MsgCapsule{
		{Data: []byte("one")},
		{Data: []byte("two")},
		{Data: []byte("three")},
	}
	for _, message := range messages {
		box.subCh <- message
	}

	for _, subscriber := range []*Subscriber{first, second} {
		for _, want := range messages {
			select {
			case got := <-subscriber.Messages():
				if got != want {
					t.Fatalf("received message %p, want %p", got, want)
				}
			case <-time.After(time.Second):
				t.Fatal("timed out waiting for fan-out message")
			}
		}
	}
}

func TestBoxFanOutRemovesOnlySlowSubscriber(t *testing.T) {
	box := newTestBox(t)
	slow := joinTestSubscriber(t, box, "slow")
	healthy := joinTestSubscriber(t, box, "healthy")

	for range cap(slow.messages) {
		slow.messages <- &pb.MsgCapsule{Data: []byte("queued")}
	}

	first := &pb.MsgCapsule{Data: []byte("first")}
	box.subCh <- first

	select {
	case <-slow.Done():
		if !errors.Is(slow.Err(), ErrSlowSubscriber) {
			t.Fatalf("slow subscriber error = %v, want %v", slow.Err(), ErrSlowSubscriber)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for slow subscriber removal")
	}

	select {
	case got := <-healthy.Messages():
		if got != first {
			t.Fatalf("healthy subscriber received %p, want %p", got, first)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy subscriber was blocked by slow subscriber")
	}

	second := &pb.MsgCapsule{Data: []byte("second")}
	box.subCh <- second
	select {
	case got := <-healthy.Messages():
		if got != second {
			t.Fatalf("healthy subscriber received %p, want %p", got, second)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy subscriber did not receive message after overflow")
	}

	assertOperationCompletes(t, func() error { return box.LeaveSub("slow") })
	assertOperationCompletes(t, func() error { return box.LeaveSub("slow") })
	late := joinTestSubscriber(t, box, "late")
	if late == nil {
		t.Fatal("joining a subscriber after overflow returned nil")
	}
}

func TestBoxCloseStopsSubscriber(t *testing.T) {
	box := newTestBox(t)
	subscriber := joinTestSubscriber(t, box, "subscriber")

	box.cancel()
	box.wg.Wait()

	select {
	case <-subscriber.Done():
		if !errors.Is(subscriber.Err(), ErrBoxClosed) {
			t.Fatalf("subscriber error = %v, want %v", subscriber.Err(), ErrBoxClosed)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscriber to stop")
	}
}

func newTestBox(t *testing.T) *Box {
	t.Helper()
	logger := zerolog.Nop()
	box, err := NewBox(&logger, "test-topic", nil)
	if err != nil {
		t.Fatalf("NewBox() error = %v", err)
	}
	// Prevent JoinSub from starting a real libp2p subscription. The operation
	// channel provides the synchronization before the actor reads this field.
	box.sub = &pubsub.Subscription{}
	t.Cleanup(func() {
		box.cancel()
		box.wg.Wait()
	})
	return box
}

func joinTestSubscriber(t *testing.T, box *Box, subscriberID string) *Subscriber {
	t.Helper()
	subscriber, err := box.JoinSub(subscriberID)
	if err != nil {
		t.Fatalf("JoinSub(%q) error = %v", subscriberID, err)
	}
	return subscriber
}

func assertOperationCompletes(t *testing.T, operation func() error) {
	t.Helper()
	result := make(chan error, 1)
	go func() {
		result <- operation()
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("operation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("operation blocked")
	}
}

func TestJoinSubAfterClose(t *testing.T) {
	logger := zerolog.Nop()
	box, err := NewBox(&logger, "closed-topic", nil)
	if err != nil {
		t.Fatalf("NewBox() error = %v", err)
	}
	box.cancel()
	box.wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := box.JoinSub("late")
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, ErrBoxClosed) {
			t.Fatalf("JoinSub() error = %v, want %v", err, ErrBoxClosed)
		}
	case <-ctx.Done():
		t.Fatal("JoinSub blocked after Box closed")
	}
}
