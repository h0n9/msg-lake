package msg

import (
	"context"
	"errors"
	"fmt"
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/rs/zerolog"
)

var ErrCenterClosed = errors.New("msg center is closed")

type topicEntry struct {
	ready       chan struct{}
	done        chan struct{}
	box         *Box
	err         error
	cleanupOnce sync.Once
	cleanupErr  error
	closing     bool
}

type Center struct {
	ctx         context.Context
	logger      *zerolog.Logger
	ps          *pubsub.PubSub
	createBoxFn func(string) (*Box, error)
	mu          sync.RWMutex
	entries     map[string]*topicEntry
	closed      bool
	closeOnce   sync.Once
	closeErr    error
}

func NewCenter(ctx context.Context, logger *zerolog.Logger, ps *pubsub.PubSub) *Center {
	subLogger := logger.With().Str("module", "msg-center").Logger()
	return &Center{ctx: ctx, logger: &subLogger, ps: ps, entries: make(map[string]*topicEntry)}
}

func (center *Center) GetBox(topicID string) (*Box, error) {
	for {
		center.mu.Lock()
		if center.closed {
			center.mu.Unlock()
			return nil, ErrCenterClosed
		}
		if center.entries == nil {
			center.entries = make(map[string]*topicEntry)
		}
		entry := center.entries[topicID]
		if entry == nil {
			entry = &topicEntry{ready: make(chan struct{}), done: make(chan struct{})}
			center.entries[topicID] = entry
			center.mu.Unlock()
			create := center.createBox
			if center.createBoxFn != nil {
				create = center.createBoxFn
			}
			box, err := create(topicID)
			center.mu.Lock()
			entry.box, entry.err = box, err
			close(entry.ready)
			if err != nil {
				if !entry.closing {
					if center.entries[topicID] == entry {
						delete(center.entries, topicID)
					}
					entry.cleanupErr = err
					close(entry.done)
				}
			}
			center.mu.Unlock()
			return box, err
		}
		center.mu.Unlock()
		<-entry.ready
		center.mu.RLock()
		closing := entry.closing
		center.mu.RUnlock()
		if closing {
			<-entry.done
			if entry.cleanupErr != nil {
				return nil, entry.cleanupErr
			}
			continue
		}
		select {
		case <-entry.done:
			if entry.err != nil {
				return nil, entry.err
			}
			continue
		default:
			return entry.box, entry.err
		}
	}
}

func (center *Center) createBox(topicID string) (*Box, error) {
	if err := center.ps.RegisterTopicValidator(topicID, newTopicValidator(topicID), pubsub.WithValidatorInline(true)); err != nil {
		return nil, err
	}
	topic, err := center.ps.Join(topicID)
	if err != nil {
		_ = center.ps.UnregisterTopicValidator(topicID)
		return nil, err
	}
	box, err := NewBox(center.logger, topicID, topic)
	if err != nil {
		_ = topic.Close()
		_ = center.ps.UnregisterTopicValidator(topicID)
		return nil, err
	}
	return box, nil
}

func (center *Center) cleanupEntry(topicID string, entry *topicEntry) error {
	center.mu.Lock()
	select {
	case <-entry.done:
		err := entry.cleanupErr
		center.mu.Unlock()
		return err
	default:
	}
	entry.closing = true
	center.mu.Unlock()
	entry.cleanupOnce.Do(func() {
		<-entry.ready
		if entry.box != nil {
			entry.cleanupErr = errors.Join(entry.box.Close(), center.ps.UnregisterTopicValidator(topicID))
		} else {
			entry.cleanupErr = entry.err
		}
		center.mu.Lock()
		if center.entries[topicID] == entry {
			if entry.cleanupErr == nil {
				delete(center.entries, topicID)
			}
		}
		center.mu.Unlock()
		select {
		case <-entry.done:
		default:
			close(entry.done)
		}
	})
	<-entry.done
	return entry.cleanupErr
}

func (center *Center) LeaveBox(topicID string) error {
	center.mu.Lock()
	entry := center.entries[topicID]
	if entry == nil {
		center.mu.Unlock()
		return fmt.Errorf("failed to find msg box with topic id '%s'", topicID)
	}
	center.mu.Unlock()
	return center.cleanupEntry(topicID, entry)
}

func (center *Center) Close() error {
	center.closeOnce.Do(func() {
		center.mu.Lock()
		center.closed = true
		entries := make(map[string]*topicEntry, len(center.entries))
		for id, entry := range center.entries {
			entries[id] = entry
		}
		center.mu.Unlock()
		var wg sync.WaitGroup
		errorsCh := make(chan error, len(entries))
		for id, entry := range entries {
			wg.Add(1)
			go func(id string, entry *topicEntry) {
				defer wg.Done()
				if err := center.cleanupEntry(id, entry); err != nil {
					errorsCh <- err
				}
			}(id, entry)
		}
		wg.Wait()
		close(errorsCh)
		for err := range errorsCh {
			center.closeErr = errors.Join(center.closeErr, err)
		}
	})
	return center.closeErr
}
