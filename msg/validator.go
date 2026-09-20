package msg

import (
	"context"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"google.golang.org/protobuf/proto"

	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/protocol"
)

func newTopicValidator(topicID string) pubsub.ValidatorEx {
	return func(_ context.Context, _ peer.ID, message *pubsub.Message) pubsub.ValidationResult {
		return validateTopicMessage(topicID, message)
	}
}

func validateTopicMessage(topicID string, message *pubsub.Message) pubsub.ValidationResult {
	if message == nil || message.Message == nil {
		return pubsub.ValidationReject
	}
	timestamped := &pb.TimestampedSignedMsgCapsule{}
	if err := proto.Unmarshal(message.GetData(), timestamped); err != nil {
		return pubsub.ValidationReject
	}
	if err := protocol.VerifyTimestampedSignedMsgCapsule(timestamped, topicID); err != nil {
		return pubsub.ValidationReject
	}
	return pubsub.ValidationAccept
}
