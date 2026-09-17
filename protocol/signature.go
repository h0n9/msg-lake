package protocol

import (
	"errors"
	"fmt"

	"github.com/postie-labs/go-postie-lib/crypto"
	"google.golang.org/protobuf/proto"

	pb "github.com/h0n9/msg-lake/proto"
)

var deterministicMarshal = proto.MarshalOptions{Deterministic: true}

// MsgCapsuleSigningBytes returns the canonical bytes covered by a publish
// signature.
func MsgCapsuleSigningBytes(msgCapsule *pb.MsgCapsule) ([]byte, error) {
	if msgCapsule == nil {
		return nil, errors.New("msg capsule is required")
	}
	data, err := deterministicMarshal.Marshal(msgCapsule)
	if err != nil {
		return nil, fmt.Errorf("marshal msg capsule: %w", err)
	}
	return data, nil
}

// SubscribeSigningBytes returns the UTF-8 bytes covered by a subscribe
// signature. Go strings already contain UTF-8 bytes without an extra encoding
// step.
func SubscribeSigningBytes(topicID string) []byte {
	return []byte(topicID)
}

// VerifySignedMsgCapsule verifies the client signature over the deterministic
// protobuf encoding of MsgCapsule.
func VerifySignedMsgCapsule(signed *pb.SignedMsgCapsule) error {
	if signed == nil {
		return errors.New("signed msg capsule is required")
	}
	if signed.GetMsgCapsule() == nil {
		return errors.New("msg capsule is required")
	}
	return verify(MsgCapsuleSigningBytes, signed.GetMsgCapsule(), signed.GetSignature())
}

// VerifyTimestampedSignedMsgCapsule verifies the client signature and ensures
// the signed topic is the topic on which the agent received the message.
func VerifyTimestampedSignedMsgCapsule(timestamped *pb.TimestampedSignedMsgCapsule, expectedTopicID string) error {
	if timestamped == nil {
		return errors.New("timestamped signed msg capsule is required")
	}
	signed := timestamped.GetSignedMsgCapsule()
	if signed == nil || signed.GetMsgCapsule() == nil {
		return errors.New("signed msg capsule is required")
	}
	if got := signed.GetMsgCapsule().GetTopicId(); got != expectedTopicID {
		return fmt.Errorf("topic id mismatch: got %q, want %q", got, expectedTopicID)
	}
	return VerifySignedMsgCapsule(signed)
}

// VerifySubscribe verifies a signature over the UTF-8 bytes of topicID.
func VerifySubscribe(topicID string, signature *pb.Signature) error {
	return verify(func(topic *string) ([]byte, error) {
		return SubscribeSigningBytes(*topic), nil
	}, &topicID, signature)
}

func verify[T any](signingBytes func(T) ([]byte, error), value T, signature *pb.Signature) error {
	if signature == nil {
		return errors.New("signature is required")
	}
	pubKeyBytes := signature.GetPubKey()
	if len(pubKeyBytes) == 0 {
		return errors.New("public key is required")
	}
	pubKey, err := crypto.GenPubKeyFromBytes(pubKeyBytes)
	if err != nil {
		return fmt.Errorf("parse public key: %w", err)
	}
	data, err := signingBytes(value)
	if err != nil {
		return err
	}
	if !pubKey.Verify(data, signature.GetData()) {
		return errors.New("invalid signature")
	}
	return nil
}
