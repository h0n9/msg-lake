package client

import (
	"crypto/tls"
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
func (c *Client) Subscribe(topicID string) error {
	// ...
	return nil
}

// Publish() publishes a message to a topic
func (c *Client) Publish(topicID, message string) error {
	// ...
	return nil
}
