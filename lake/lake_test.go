package lake

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/h0n9/msg-lake/msg"
)

func TestSubscriberStreamError(t *testing.T) {
	t.Run("slow subscriber", func(t *testing.T) {
		err := subscriberStreamError(msg.ErrSlowSubscriber)
		if got := status.Code(err); got != codes.ResourceExhausted {
			t.Fatalf("status code = %v, want %v", got, codes.ResourceExhausted)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		if err := subscriberStreamError(context.Canceled); err != nil {
			t.Fatalf("subscriberStreamError(context.Canceled) = %v, want nil", err)
		}
	})

	t.Run("other error", func(t *testing.T) {
		want := errors.New("send failed")
		if got := subscriberStreamError(want); !errors.Is(got, want) {
			t.Fatalf("subscriberStreamError() = %v, want %v", got, want)
		}
	})
}
