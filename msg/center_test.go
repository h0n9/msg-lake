package msg

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/rs/zerolog"
)

func TestCenterGetBoxConcurrentCreation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h, err := libp2p.New(libp2p.NoListenAddrs)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	center := NewCenter(ctx, &logger, ps)

	const callers = 100
	boxes := make(chan *Box, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			box, err := center.GetBox("shared-topic")
			boxes <- box
			errs <- err
		}()
	}
	wg.Wait()
	close(boxes)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("GetBox() error = %v", err)
		}
	}

	var first *Box
	for box := range boxes {
		if first == nil {
			first = box
			continue
		}
		if box != first {
			t.Fatalf("GetBox() returned different boxes: %p and %p", first, box)
		}
	}
	if first == nil {
		t.Fatal("GetBox() returned no box")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Box.Close() error = %v", err)
	}
}

func BenchmarkMapLookupExisting(b *testing.B) {
	for _, parallel := range []bool{false, true} {
		b.Run(fmt.Sprintf("parallel=%t", parallel), func(b *testing.B) {
			boxes := map[string]*Box{"topic": {topicID: "topic"}}
			b.ReportAllocs()
			b.ResetTimer()
			if parallel {
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						if boxes["topic"] == nil {
							b.Fatal("missing box")
						}
					}
				})
				return
			}
			for range b.N {
				if boxes["topic"] == nil {
					b.Fatal("missing box")
				}
			}
		})
	}
}

func BenchmarkCenterGetBoxExisting(b *testing.B) {
	for _, topicCount := range []int{1, 100, 10_000} {
		b.Run(fmt.Sprintf("topics=%d", topicCount), func(b *testing.B) {
			center := &Center{boxes: make(map[string]*Box, topicCount)}
			topicIDs := make([]string, topicCount)
			for i := range topicCount {
				topicID := fmt.Sprintf("topic-%d", i)
				topicIDs[i] = topicID
				center.boxes[topicID] = &Box{topicID: topicID}
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				box, err := center.GetBox(topicIDs[i%topicCount])
				if err != nil || box == nil {
					b.Fatalf("GetBox() = (%v, %v)", box, err)
				}
			}
		})
	}
}

func BenchmarkCenterGetBoxExistingParallel(b *testing.B) {
	for _, topicCount := range []int{1, 100, 10_000} {
		b.Run(fmt.Sprintf("topics=%d", topicCount), func(b *testing.B) {
			center := &Center{boxes: make(map[string]*Box, topicCount)}
			topicIDs := make([]string, topicCount)
			for i := range topicCount {
				topicID := fmt.Sprintf("topic-%d", i)
				topicIDs[i] = topicID
				center.boxes[topicID] = &Box{topicID: topicID}
			}

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					box, err := center.GetBox(topicIDs[i%topicCount])
					if err != nil || box == nil {
						b.Fatalf("GetBox() = (%v, %v)", box, err)
					}
					i++
				}
			})
		})
	}
}

func TestCenterCloseRejectsNewBoxesAndIsIdempotent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := libp2p.New(libp2p.NoListenAddrs)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	center := NewCenter(ctx, &logger, ps)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := center.GetBox(id); err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { results <- center.Close() }()
	}
	for i := 0; i < 8; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := center.GetBox("a"); !errors.Is(err, ErrCenterClosed) {
		t.Fatalf("GetBox after close = %v", err)
	}
}

func TestCenterClosePropagatesBoxFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, err := libp2p.New(libp2p.NoListenAddrs)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	center := NewCenter(ctx, &logger, ps)
	box, err := center.GetBox("failure")
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("topic close failed")
	box.topic = &fakeTopic{closeErr: want}
	if err := center.Close(); !errors.Is(err, want) {
		t.Fatalf("Center.Close error = %v, want %v", err, want)
	}
	if err := center.Close(); !errors.Is(err, want) {
		t.Fatalf("repeated Center.Close error = %v, want %v", err, want)
	}
	if _, err := center.GetBox("failure"); !errors.Is(err, ErrCenterClosed) {
		t.Fatalf("GetBox after failed close = %v", err)
	}
}

func TestCenterCreationFailureWhileLeaveWaits(t *testing.T) {
	want := errors.New("creation failed")
	entered := make(chan struct{})
	release := make(chan struct{})
	center := &Center{boxes: make(map[string]*Box), entries: make(map[string]*topicEntry), closeDone: make(chan struct{})}
	center.createBoxFn = func(string) (*Box, error) { close(entered); <-release; return nil, want }
	created := make(chan error, 1)
	go func() { _, err := center.GetBox("topic"); created <- err }()
	<-entered
	left := make(chan error, 1)
	go func() { left <- center.LeaveBox("topic") }()
	deadline := time.After(time.Second)
	for {
		center.mu.RLock()
		closing := center.entries["topic"].closing
		center.mu.RUnlock()
		if closing {
			break
		}
		select {
		case <-deadline:
			t.Fatal("LeaveBox did not enter cleanup")
		default:
			runtime.Gosched()
		}
	}
	close(release)
	if err := <-created; !errors.Is(err, want) {
		t.Fatalf("GetBox error = %v", err)
	}
	if err := <-left; !errors.Is(err, want) {
		t.Fatalf("LeaveBox error = %v", err)
	}
	if _, err := center.GetBox("topic"); !errors.Is(err, want) {
		t.Fatalf("failed entry error = %v", err)
	}
}

func TestStaleCleanupDoesNotDeleteReplacementBox(t *testing.T) {
	center := &Center{boxes: make(map[string]*Box), entries: make(map[string]*topicEntry)}
	replacement := &Box{topicID: "topic"}
	current := &topicEntry{ready: make(chan struct{}), done: make(chan struct{}), box: replacement}
	center.entries["topic"] = current
	center.boxes["topic"] = replacement
	stale := &topicEntry{ready: make(chan struct{}), done: make(chan struct{}), err: errors.New("old creation failed")}
	close(stale.ready)
	if err := center.cleanupEntry("topic", stale); !errors.Is(err, stale.err) {
		t.Fatalf("cleanup error = %v", err)
	}
	if center.boxes["topic"] != replacement || center.entries["topic"] != current {
		t.Fatal("stale cleanup removed replacement box")
	}
}
