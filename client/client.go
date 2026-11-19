package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/status"

	pb "github.com/h0n9/msg-lake/proto"
	"github.com/h0n9/msg-lake/protocol"
	"github.com/postie-labs/go-postie-lib/crypto"
)

type Client struct {
	privKey                *crypto.PrivKey
	grpcClientConn         *grpc.ClientConn
	msgLakeClient          pb.MsgLakeClient
	verifyReceivedMessages bool
	closed                 context.Context
	cancel                 context.CancelFunc
	closeOnce              sync.Once
	ackTimeout             time.Duration
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
		ackTimeout:     5 * time.Second,
	}
	client.closed, client.cancel = context.WithCancel(context.Background())
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	return client, nil
}

// Close() closes the grpc client connection
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		c.cancel()
		_ = c.grpcClientConn.Close()
	})
}

// Subscribe() subscribes to a topic
func (c *Client) Subscribe(ctx context.Context, topicID string, timestampedMsgCapsuleHandler func(*pb.TimestampedSignedMsgCapsule) error) error {
	// sign the UTF-8 bytes of topicID
	sigDataBytes, err := c.privKey.Sign(protocol.SubscribeSigningBytes(topicID))
	if err != nil {
		return err
	}

	request := &pb.SubscribeReq{
		TopicId: topicID,
		Signature: &pb.Signature{
			PubKey: c.privKey.PubKey().Bytes(),
			Data:   sigDataBytes,
		},
	}
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(c.closed, cancel)
	defer stop()
	backoff := 100 * time.Millisecond
	for {
		if stopped, err := subscriptionStopped(ctx, c.closed); stopped {
			return err
		}
		ackedAt, err := c.subscribeAttempt(subCtx, request, topicID, timestampedMsgCapsuleHandler)
		if stopped, contextErr := subscriptionStopped(ctx, c.closed); stopped {
			return contextErr
		}
		if !retrySubscription(err) {
			return err
		}
		if !ackedAt.IsZero() && time.Since(ackedAt) >= 30*time.Second {
			backoff = 100 * time.Millisecond
		}
		wait := backoff + time.Duration(float64(backoff)*(rand.Float64()*0.4-0.2))
		if wait > 5*time.Second {
			wait = 5 * time.Second
		}
		timer := time.NewTimer(wait)
		select {
		case <-subCtx.Done():
			timer.Stop()
			_, err := subscriptionStopped(ctx, c.closed)
			return err
		case <-timer.C:
		}
		if backoff < 5*time.Second {
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
}

var errACKTimeout = errors.New("subscribe ACK timed out")

func subscriptionStopped(ctx, closed context.Context) (bool, error) {
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			return true, nil
		}
		return true, ctx.Err()
	}
	if closed.Err() != nil {
		return true, nil
	}
	return false, nil
}

func retrySubscription(err error) bool {
	return errors.Is(err, errACKTimeout) || errors.Is(err, io.EOF) || status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded
}

func (c *Client) subscribeAttempt(ctx context.Context, request *pb.SubscribeReq, topicID string, handler func(*pb.TimestampedSignedMsgCapsule) error) (time.Time, error) {
	attemptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	expired := make(chan struct{})
	timer := time.AfterFunc(c.ackTimeout, func() { close(expired); cancel() })
	defer timer.Stop()
	stream, err := c.msgLakeClient.Subscribe(attemptCtx, request)
	if err != nil {
		return time.Time{}, classifyACKError(err, timer, expired)
	}

	// block until receive subscribe ack msg
	subRes, err := stream.Recv()
	if err != nil {
		return time.Time{}, classifyACKError(err, timer, expired)
	}
	if !timer.Stop() {
		<-expired
		return time.Time{}, errACKTimeout
	}

	// check subscribe ack msg
	if subRes.GetType() != pb.SubscribeResType_SUBSCRIBE_RES_TYPE_ACK {
		return time.Time{}, fmt.Errorf("failed to receive subscribe ack from agent")
	}
	if !subRes.GetOk() {
		return time.Time{}, fmt.Errorf("failed to begin subscribing msgs")
	}
	ackedAt := time.Now()

	for {
		select {
		case <-ctx.Done():
			return ackedAt, ctx.Err()
		default:
			res, err := stream.Recv()
			if err != nil {
				return ackedAt, err
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
					return ackedAt, fmt.Errorf("verify received msg capsule: %w", err)
				}
			}

			// handle the received timestamped capsule
			err = handler(timestamped)
			if err != nil {
				fmt.Println(err)
			}
		}
	}
}

func classifyACKError(err error, timer *time.Timer, expired <-chan struct{}) error {
	if !timer.Stop() {
		<-expired
		return errACKTimeout
	}
	return err
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
