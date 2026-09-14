package msg

import (
	"context"
	"fmt"
	"sync"
	"testing"

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
