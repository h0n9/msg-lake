package msg

import (
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/postie-labs/go-postie-lib/crypto"
	"google.golang.org/protobuf/proto"

	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/protocol"
)

func TestValidateTopicMessage(t *testing.T) {
	valid := validatorMessage(t, "topic")
	if got := validateTopicMessage("topic", valid); got != pubsub.ValidationAccept {
		t.Fatalf("valid message result = %v, want accept", got)
	}
	if got := validateTopicMessage("other", valid); got != pubsub.ValidationReject {
		t.Fatalf("topic mismatch result = %v, want reject", got)
	}

	tampered := validatorMessage(t, "topic")
	timestamped := &pb.TimestampedSignedMsgCapsule{}
	if err := proto.Unmarshal(tampered.GetData(), timestamped); err != nil {
		t.Fatal(err)
	}
	timestamped.SignedMsgCapsule.MsgCapsule.Data = []byte("tampered")
	tampered.Message.Data, _ = proto.Marshal(timestamped)
	if got := validateTopicMessage("topic", tampered); got != pubsub.ValidationReject {
		t.Fatalf("tampered message result = %v, want reject", got)
	}

	malformed := &pubsub.Message{Message: &pubsubpb.Message{Data: []byte{0xff}}}
	if got := validateTopicMessage("topic", malformed); got != pubsub.ValidationReject {
		t.Fatalf("malformed message result = %v, want reject", got)
	}
	if got := validateTopicMessage("topic", nil); got != pubsub.ValidationReject {
		t.Fatalf("nil message result = %v, want reject", got)
	}
}

func validatorMessage(t *testing.T, topicID string) *pubsub.Message {
	t.Helper()
	privKey, err := crypto.GenPrivKeyFromSeed([]byte("validator-publisher"))
	if err != nil {
		t.Fatal(err)
	}
	capsule := &pb.MsgCapsule{TopicId: topicID, Data: []byte("payload")}
	signingBytes, err := protocol.MsgCapsuleSigningBytes(capsule)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := privKey.Sign(signingBytes)
	if err != nil {
		t.Fatal(err)
	}
	timestamped := &pb.TimestampedSignedMsgCapsule{
		Timestamp: 1,
		SignedMsgCapsule: &pb.SignedMsgCapsule{
			MsgCapsule: capsule,
			Signature:  &pb.Signature{PubKey: privKey.PubKey().Bytes(), Data: signature},
		},
	}
	data, err := proto.Marshal(timestamped)
	if err != nil {
		t.Fatal(err)
	}
	return &pubsub.Message{Message: &pubsubpb.Message{Data: data}}
}
