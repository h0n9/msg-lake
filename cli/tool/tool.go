package tool

import (
	"github.com/bojand/ghz/runner"
	"github.com/h0n9/msg-lake/proto"
	"github.com/postie-labs/go-postie-lib/crypto"
	"github.com/spf13/cobra"
)

var (
	call       string
	hostAddr   string
	tlsEnabled bool
)

func init() {
	Cmd.Flags().StringVarP(&call, "call", "c", "Lake.Publish", "grpc method to call")
	Cmd.Flags().StringVarP(&hostAddr, "host", "H", "localhost:8080", "target host address")
	Cmd.Flags().BoolVarP(&tlsEnabled, "tls", "t", false, "enable tls connection")
}

var Cmd = &cobra.Command{
	Use:   "tool",
	Short: "run msg lake tool",
	RunE: func(cmd *cobra.Command, args []string) error {
		privKey, err := crypto.GenPrivKeyFromSeed([]byte("test"))
		if err != nil {
			return err
		}
		pubKeyBytes := privKey.PubKey().Bytes()
		data := []byte("hello world")
		sigBytes, err := privKey.Sign(data)
		if err != nil {
			return err
		}

		pubReq := proto.PublishReq{
			TopicId: "test",
			MsgCapsule: &proto.MsgCapsule{
				Data: data,
				Signature: &proto.Signature{
					PubKey: pubKeyBytes,
					Data:   sigBytes,
				},
			},
		}
		_, err = runner.Run(
			call,
			hostAddr,
			runner.WithInsecure(!tlsEnabled),
			runner.WithData(&pubReq),
		)
		if err != nil {
			return err
		}

		return nil
	},
}
