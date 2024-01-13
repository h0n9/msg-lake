package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/postie-labs/go-postie-lib/crypto"

	pb "github.com/h0n9/msg-lake/proto"
)

var (
	tlsEnabled bool
	hostAddr   string
	topicID    string
	nickname   string
)

var Cmd = &cobra.Command{
	Use:   "client",
	Short: "run msg lake client (interactive)",
	RunE: func(cmd *cobra.Command, args []string) error {
		var (
			conn *grpc.ClientConn
		)
		// init wg
		wg := sync.WaitGroup{}

		// init sig channel
		sigCh := make(chan os.Signal, 1)
		defer close(sigCh)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		// init ctx with cancel
		ctx, cancel := context.WithCancel(context.Background())

		// listen signals
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return
			case s := <-sigCh:
				fmt.Printf("got signal %v, attempting graceful shutdown\n", s)
				if conn != nil {
					fmt.Printf("closing grpc client ... ")
					conn.Close()
					fmt.Printf("done\n")
				}
				fmt.Printf("cancelling ctx ... ")
				cancel()
				fmt.Printf("done\n")
			}
		}()

		// init privKey
		privKey, err := crypto.GenPrivKeyFromSeed([]byte(nickname))
		if err != nil {
			return err
		}
		pubKeyBytes := privKey.PubKey().Bytes()

		// init grpc client
		creds := grpc.WithTransportCredentials(insecure.NewCredentials())
		if tlsEnabled {
			creds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{}))
		}
		conn, err = grpc.Dial(hostAddr, creds)
		if err != nil {
			return err
		}
		cli := pb.NewLakeClient(conn)

		msg := Msg{
			Data: []byte(topicID),
		}
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		sigDataBytes, err := privKey.Sign(data)
		if err != nil {
			return err
		}

		stream, err := cli.Subscribe(ctx, &pb.SubscribeReq{
			TopicId: topicID,
			MsgCapsule: &pb.MsgCapsule{
				Data: data,
				Signature: &pb.Signature{
					PubKey: pubKeyBytes,
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

		// execute goroutine (receiver)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				res, err := stream.Recv()
				if err != nil {
					fmt.Println(err)
					cancel()
					return
				}
				if res.GetType() != pb.SubscribeResType_SUBSCRIBE_RES_TYPE_RELAY {
					continue
				}
				msgCapsule := res.GetMsgCapsule()
				signature := msgCapsule.GetSignature()
				if bytes.Equal(signature.GetPubKey(), pubKeyBytes) {
					continue
				}
				if len(msgCapsule.GetData()) == 0 {
					continue
				}
				printOutput(true, msgCapsule)
				printInput(true)
			}
		}()

		// execute goroutine (sender)
		wg.Add(1)
		go func() {
			defer wg.Done()
			reader := bufio.NewReader(os.Stdin)
			for {
				select {
				case <-ctx.Done():
					return
				default:
					printInput(false)
					input, err := reader.ReadString('\n')
					if err == io.EOF {
						return
					}
					if err != nil {
						fmt.Println(err)
						continue
					}
					input = strings.TrimSuffix(input, "\n")
					if input == "" {
						continue
					}
					go func() {
						data, err := json.Marshal(input)
						if err != nil {
							fmt.Println(err)
							return
						}
						sigDataBytes, err := privKey.Sign(data)
						if err != nil {
							fmt.Println(err)
							return
						}

						pubRes, err := cli.Publish(ctx, &pb.PublishReq{
							TopicId: topicID,
							MsgCapsule: &pb.MsgCapsule{
								Data: data,
								Signature: &pb.Signature{
									PubKey: pubKeyBytes,
									Data:   sigDataBytes,
								},
							},
						})
						if err != nil {
							fmt.Println(err)
							return
						}

						// check publish res
						if !pubRes.GetOk() {
							fmt.Println("failed to send message")
							return
						}
					}()
				}
			}
		}()

		wg.Wait()

		return nil
	},
}

func printInput(newline bool) {
	s := "💬 <%s> "
	if newline {
		s = "\r\n" + s
	}
	fmt.Printf(s, "me")
}

func printOutput(newline bool, msgCapsule *pb.MsgCapsule) {
	s := "📩 <%s> [%d] %s"
	if newline {
		s = "\r\n" + s
	}
	fmt.Printf(
		s,
		fmt.Sprintf("%x", msgCapsule.GetSignature().GetPubKey()[:4]),
		msgCapsule.GetTimestamp(),
		msgCapsule.GetData(),
	)
}

type Msg struct {
	Data     []byte            `json:"data"`
	Metadata map[string][]byte `json:"metadata"`
}

func init() {
	r := rand.New(rand.NewSource(time.Now().UnixMicro())).Int()

	Cmd.Flags().BoolVarP(&tlsEnabled, "tls", "t", false, "enable tls connection")
	Cmd.Flags().StringVar(&hostAddr, "host", "localhost:8080", "host addr")
	Cmd.Flags().StringVar(&topicID, "topic", "life is beautiful", "topic id")
	Cmd.Flags().StringVarP(&nickname, "nickname", "n", fmt.Sprintf("alien-%d", r), "consumer id")
}
