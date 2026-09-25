package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"

	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/protocol"
	"github.com/postie-labs/go-postie-lib/crypto"
)

type Client struct {
	privKey                *crypto.PrivKey
	grpcClientConn         *grpc.ClientConn
	msgLakeClient          pb.MsgLakeClient
	verifyReceivedMessages bool
}

type Option func(*Client)

// WithReceivedMessageVerification controls end-to-end signature verification
// before Subscribe invokes its message handler. It is disabled by default.
func WithReceivedMessageVerification(enabled bool) Option {
	return func(client *Client) {
		client.verifyReceivedMessages = enabled
	}
}

func NewClient(privKey *crypto.PrivKey, hostAddr string, tlsEnabled bool, opts ...Option) (*Client, error) {
	// init grpc client
	creds := grpc.WithTransportCredentials(insecure.NewCredentials())
	if tlsEnabled {
		creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
	}
	grpcClientConn, err := grpc.Dial(hostAddr, creds)
	if err != nil {
		return nil, err
	}

	// init msg lake client
	msgLakeClient := pb.NewMsgLakeClient(grpcClientConn)

	client := &Client{
		privKey:        privKey,
		grpcClientConn: grpcClientConn,
		msgLakeClient:  msgLakeClient,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client, nil
}

// Close() closes the grpc client connection
func (c *Client) Close() {
	err := c.grpcClientConn.Close()
	if err != nil {
		fmt.Printf("failed to close grpc client: %v\n", err)
	}
}

// Subscribe() subscribes to a topic
func (c *Client) Subscribe(ctx context.Context, topicID string, timestampedMsgCapsuleHandler func(*pb.TimestampedSignedMsgCapsule) error) error {
	// sign the UTF-8 bytes of topicID
	sigDataBytes, err := c.privKey.Sign(protocol.SubscribeSigningBytes(topicID))
	if err != nil {
		return err
	}

	// subscribe to the topic
	stream, err := c.msgLakeClient.Subscribe(ctx, &pb.SubscribeReq{
		TopicId: topicID,
		Signature: &pb.Signature{
			PubKey: c.privKey.PubKey().Bytes(),
			Data:   sigDataBytes,
		},
	})
	if err != nil {
		return err
	}

	// block until receive subscribe ack msg
	subRes, err := stream.Recv()
	if err != nil {
		return err
	}

	// check subscribe ack msg
	if subRes.GetType() != pb.SubscribeResType_SUBSCRIBE_RES_TYPE_ACK {
		return fmt.Errorf("failed to receive subscribe ack from agent")
	}
	if !subRes.GetOk() {
		return fmt.Errorf("failed to begin subscribing msgs")
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			res, err := stream.Recv()
			if err != nil {
				return err
			}

			// check if the received message is a relay message
			if res.GetType() != pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY {
				continue
			}

			// get a timestamped capsule from the received message
			timestamped := res.GetTimestampedSignedMsgCapsule()

			if timestamped == nil {
				continue
			}
			if c.verifyReceivedMessages {
				if err := protocol.VerifyTimestampedSignedMsgCapsule(timestamped, topicID); err != nil {
					return fmt.Errorf("verify received msg capsule: %w", err)
				}
			}

			// handle the received timestamped capsule
			err = timestampedMsgCapsuleHandler(timestamped)
			if err != nil {
				fmt.Println(err)
			}
		}
	}
}

// Publish() publishes a message to a topic
func (c *Client) Publish(ctx context.Context, topicID, message string) error {
	// serialize the application message
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	msgCapsule := &pb.MsgCapsule{
		TopicId: topicID,
		Data:    data,
	}
	signingBytes, err := protocol.MsgCapsuleSigningBytes(msgCapsule)
	if err != nil {
		return err
	}

	// sign the deterministic protobuf encoding of MsgCapsule
	sigDataBytes, err := c.privKey.Sign(signingBytes)
	if err != nil {
		return err
	}

	// publish the message
	pubRes, err := c.msgLakeClient.Publish(
		ctx,
		&pb.PublishReq{
			SignedMsgCapsule: &pb.SignedMsgCapsule{
				MsgCapsule: msgCapsule,
				Signature: &pb.Signature{
					PubKey: c.privKey.PubKey().Bytes(),
					Data:   sigDataBytes,
				},
			},
		},
		grpc.UseCompressor(gzip.Name),
	)
	if err != nil {
		return err
	}

	// check publish ack msg
	if !pubRes.GetOk() {
		return fmt.Errorf("failed to publish msg")
	}

	return nil
}

// VerifyReceivedMessage verifies a relayed client signature and topic binding.
func VerifyReceivedMessage(msg *pb.TimestampedSignedMsgCapsule, expectedTopicID string) error {
	return protocol.VerifyTimestampedSignedMsgCapsule(msg, expectedTopicID)
}
