package client

import (
	"testing"

	"github.com/postie-labs/go-postie-lib/crypto"

	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/protocol"
)

func TestWithReceivedMessageVerification(t *testing.T) {
	client := &Client{}
	WithReceivedMessageVerification(true)(client)
	if !client.verifyReceivedMessages {
		t.Fatal("received message verification was not enabled")
	}
}

func TestVerifyReceivedMessage(t *testing.T) {
	privKey, err := crypto.GenPrivKeyFromSeed([]byte("client-test"))
	if err != nil {
		t.Fatal(err)
	}
	capsule := &pb.MsgCapsule{TopicId: "topic", Data: []byte("payload")}
	signingBytes, err := protocol.MsgCapsuleSigningBytes(capsule)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := privKey.Sign(signingBytes)
	if err != nil {
		t.Fatal(err)
	}
	message := &pb.TimestampedSignedMsgCapsule{
		Timestamp: 1,
		SignedMsgCapsule: &pb.SignedMsgCapsule{
			MsgCapsule: capsule,
			Signature:  &pb.Signature{PubKey: privKey.PubKey().Bytes(), Data: signature},
		},
	}
	if err := VerifyReceivedMessage(message, "topic"); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}
	if err := VerifyReceivedMessage(message, "other"); err == nil {
		t.Fatal("message accepted for another topic")
	}
	message.SignedMsgCapsule.MsgCapsule.Data = []byte("tampered")
	if err := VerifyReceivedMessage(message, "topic"); err == nil {
		t.Fatal("tampered message passed verification")
	}
}
