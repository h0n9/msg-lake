package msg

import (
	"context"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/rs/zerolog"
)

type Center struct {
	ctx    context.Context
	logger *zerolog.Logger

	ps    *pubsub.PubSub
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
	box, exist := center.boxes[topicID]
	if !exist {
		topic, err := center.ps.Join(topicID)
		if err != nil {
			return nil, err
		}
		newBox, err := NewBox(center.ctx, center.logger, topicID, topic)
		if err != nil {
			return nil, err
		}
		box = newBox
		center.boxes[topicID] = box
	}
	return box, nil
}
