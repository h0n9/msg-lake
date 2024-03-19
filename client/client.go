package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/h0n9/msg-lake/proto"
	"github.com/postie-labs/go-postie-lib/crypto"
)

type Client struct {
	privKey        *crypto.PrivKey
	grpcClientConn *grpc.ClientConn
	msgLakeClient  pb.MsgLakeClient
}

func NewClient(privKey *crypto.PrivKey, hostAddr string, tlsEnabled bool) (*Client, error) {
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

	return &Client{
		privKey:        privKey,
		grpcClientConn: grpcClientConn,
		msgLakeClient:  msgLakeClient,
	}, nil
}

// Close() closes the grpc client connection
func (c *Client) Close() {
	err := c.grpcClientConn.Close()
	if err != nil {
		fmt.Printf("failed to close grpc client: %v\n", err)
	}
}

// Subscribe() subscribes to a topic
func (c *Client) Subscribe(ctx context.Context, topicID string, msgCapsuleHandler func(*pb.MsgCapsule) error) error {
	// serialize topicID
	data, err := json.Marshal(topicID)
	if err != nil {
		return err
	}

	// sign the serialized topicID
	sigDataBytes, err := c.privKey.Sign(data)
	if err != nil {
		return err
	}

	// subscribe to the topic
	stream, err := c.msgLakeClient.Subscribe(ctx, &pb.SubscribeReq{
		TopicId: topicID,
		MsgCapsule: &pb.MsgCapsule{
			Data: data,
			Signature: &pb.Signature{
				PubKey: c.privKey.PubKey().Bytes(),
				Data:   sigDataBytes,
			},
		},
	})
	if err != nil {
		return err
	}

	// block until recieve subscribe ack msg
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

			// get a msgCapsule from the received message
			msgCapsule := res.GetMsgCapsule()

			// check if the msgCapsule is empty
			if len(msgCapsule.GetData()) == 0 {
				continue
			}

			// handle the received msgCapsule
			err = msgCapsuleHandler(msgCapsule)
			if err != nil {
				fmt.Println(err)
			}
		}
	}
}

// Publish() publishes a message to a topic
func (c *Client) Publish(topicID, message string) error {
	// ...
	return nil
}
