package client

import (
	"bufio"
	"bytes"
	"context"
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

	"github.com/postie-labs/go-postie-lib/crypto"

	"github.com/h0n9/msg-lake/client"
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

		// init msg lake client
		msgLakeClient, err := client.NewClient(privKey, hostAddr, tlsEnabled)
		if err != nil {
			return err
		}

		// execute goroutine (receiver)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := msgLakeClient.Subscribe(ctx, topicID, func(msgCapsule *pb.MsgCapsule) error {
				signature := msgCapsule.GetSignature()
				if bytes.Equal(signature.GetPubKey(), pubKeyBytes) {
					return nil
				}
				if len(msgCapsule.GetData()) == 0 {
					return nil
				}
				printOutput(true, msgCapsule)
				printInput(true)
				return nil
			})
			if err != nil {
				fmt.Println(err)
				cancel()
				return
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
					err = msgLakeClient.Publish(ctx, topicID, input)
					if err != nil {
						fmt.Println(err)
					}
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
