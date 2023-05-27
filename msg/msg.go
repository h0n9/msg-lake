package msg

type SubscriberCh chan []byte

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
