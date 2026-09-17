package protocol

import (
	"bytes"
	"testing"

	"github.com/postie-labs/go-postie-lib/crypto"

	pb "github.com/h0n9/msg-lake/proto"
)

func TestMsgCapsuleSigningBytesDeterministic(t *testing.T) {
	capsule := &pb.MsgCapsule{TopicId: "topic", Data: []byte("payload"), IsEncrypted: true}
	first, err := MsgCapsuleSigningBytes(capsule)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MsgCapsuleSigningBytes(capsule)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("signing bytes differ: %x != %x", first, second)
	}
}

func TestVerifySignedMsgCapsuleRejectsMutation(t *testing.T) {
	signed := signedCapsule(t, &pb.MsgCapsule{
		TopicId:     "topic",
		Data:        []byte("payload"),
		IsEncrypted: true,
	})
	if err := VerifySignedMsgCapsule(signed); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	tests := map[string]func(*pb.MsgCapsule){
		"topic":        func(c *pb.MsgCapsule) { c.TopicId = "other" },
		"data":         func(c *pb.MsgCapsule) { c.Data = []byte("changed") },
		"is encrypted": func(c *pb.MsgCapsule) { c.IsEncrypted = false },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			clone := &pb.SignedMsgCapsule{
				MsgCapsule: &pb.MsgCapsule{
					TopicId:     signed.GetMsgCapsule().GetTopicId(),
					Data:        bytes.Clone(signed.GetMsgCapsule().GetData()),
					IsEncrypted: signed.GetMsgCapsule().GetIsEncrypted(),
				},
				Signature: signed.GetSignature(),
			}
			mutate(clone.MsgCapsule)
			if err := VerifySignedMsgCapsule(clone); err == nil {
				t.Fatal("mutated capsule passed signature verification")
			}
		})
	}
}

func TestVerifySubscribe(t *testing.T) {
	privKey, err := crypto.GenPrivKeyFromSeed([]byte("subscriber"))
	if err != nil {
		t.Fatal(err)
	}
	signatureBytes, err := privKey.Sign(SubscribeSigningBytes("topic"))
	if err != nil {
		t.Fatal(err)
	}
	signature := &pb.Signature{PubKey: privKey.PubKey().Bytes(), Data: signatureBytes}
	if err := VerifySubscribe("topic", signature); err != nil {
		t.Fatalf("valid subscribe signature rejected: %v", err)
	}
	if err := VerifySubscribe("other", signature); err == nil {
		t.Fatal("signature was accepted for another topic")
	}
}

func TestVerifyRejectsInvalidInputs(t *testing.T) {
	for name, signed := range map[string]*pb.SignedMsgCapsule{
		"nil signed capsule": nil,
		"nil capsule":        {Signature: &pb.Signature{}},
		"nil signature":      {MsgCapsule: &pb.MsgCapsule{}},
		"invalid public key": {
			MsgCapsule: &pb.MsgCapsule{},
			Signature:  &pb.Signature{PubKey: []byte("invalid"), Data: []byte("invalid")},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := VerifySignedMsgCapsule(signed); err == nil {
				t.Fatal("invalid input passed verification")
			}
		})
	}
}

func signedCapsule(t *testing.T, capsule *pb.MsgCapsule) *pb.SignedMsgCapsule {
	t.Helper()
	privKey, err := crypto.GenPrivKeyFromSeed([]byte("publisher"))
	if err != nil {
		t.Fatal(err)
	}
	signingBytes, err := MsgCapsuleSigningBytes(capsule)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := privKey.Sign(signingBytes)
	if err != nil {
		t.Fatal(err)
	}
	return &pb.SignedMsgCapsule{
		MsgCapsule: capsule,
		Signature: &pb.Signature{
			PubKey: privKey.PubKey().Bytes(),
			Data:   signature,
		},
	}
}
