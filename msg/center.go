package msg

import (
	"context"
	"fmt"
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/rs/zerolog"
)

type Center struct {
	ctx    context.Context
	logger *zerolog.Logger

	ps    *pubsub.PubSub
	mu    sync.RWMutex
	boxes map[string]*Box
}

func NewCenter(ctx context.Context, logger *zerolog.Logger, ps *pubsub.PubSub) *Center {
	subLogger := logger.With().Str("module", "msg-center").Logger()
	return &Center{
		ctx:    ctx,
		logger: &subLogger,

		ps:    ps,
		boxes: make(map[string]*Box),
	}
}

func (center *Center) GetBox(topicID string) (*Box, error) {
	center.mu.RLock()
	box, exist := center.boxes[topicID]
	center.mu.RUnlock()
	if exist {
		return box, nil
	}

	center.mu.Lock()
	defer center.mu.Unlock()

	// Another caller may have created the box while this caller was waiting
	// for the write lock.
	box, exist = center.boxes[topicID]
	if exist {
		return box, nil
	}

	err := center.ps.RegisterTopicValidator(
		topicID,
		newTopicValidator(topicID),
		pubsub.WithValidatorInline(true),
	)
	if err != nil {
		return nil, err
	}

	topic, err := center.ps.Join(topicID)
	if err != nil {
		_ = center.ps.UnregisterTopicValidator(topicID)
		return nil, err
	}
	box, err = NewBox(center.logger, topicID, topic)
	if err != nil {
		_ = topic.Close()
		_ = center.ps.UnregisterTopicValidator(topicID)
		return nil, err
	}
	center.boxes[topicID] = box

	return box, nil
}

func (center *Center) LeaveBox(topicID string) error {
	center.mu.Lock()
	defer center.mu.Unlock()

	box, exist := center.boxes[topicID]
	if !exist {
		return fmt.Errorf("failed to find msg box with topic id '%s'", topicID)
	}
	err := box.Close()
	if err != nil {
		return err
	}
	delete(center.boxes, topicID)
	if err := center.ps.UnregisterTopicValidator(topicID); err != nil {
		return err
	}
	center.logger.Debug().Str("topic-id", topicID).Msg("left")
	return nil
}
