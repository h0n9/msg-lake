package msg

import (
	pb "github.com/h0n9/msg-lake/proto"
)

type SubscriberCh chan *pb.MsgCapsule

type setSubscriber struct {
	subscriberID string
	subscriberCh SubscriberCh

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
