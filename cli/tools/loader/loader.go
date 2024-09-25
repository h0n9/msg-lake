package loader

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/postie-labs/go-postie-lib/crypto"

	"github.com/h0n9/msg-lake/client"
)

var Cmd = &cobra.Command{
	Use:   "loader",
	Short: "tool for load test",
	RunE:  runE,
}

var (
	tlsEnabled bool
	hostAddr   string
	topicID    string
	nickname   string

	interval  time.Duration
	loadCount int // 0 means unlimited
)

func runE(cmd *cobra.Command, args []string) error {
	var msgLakeClient *client.Client

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
			if msgLakeClient != nil {
				msgLakeClient.Close()
			}
			fmt.Printf("cancelling ctx ... ")
			cancel()
			fmt.Printf("done\n")
		}
	}()

	/////////////////////////////////
	// real things begin from here //
	/////////////////////////////////

	// init privKey
	privKey, err := crypto.GenPrivKeyFromSeed([]byte(nickname))
	if err != nil {
		return err
	}
	// pubKeyBytes := privKey.PubKey().Bytes()

	// init msg lake client
	msgLakeClient, err = client.NewClient(privKey, hostAddr, tlsEnabled)
	if err != nil {
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		// init ticker with interval
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		i := 0

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				fmt.Println(i)
				i += 1
			}
		}
	}()

	wg.Wait()

	return nil
}

func init() {
	r := rand.New(rand.NewSource(time.Now().UnixMicro())).Int()

	Cmd.Flags().BoolVarP(&tlsEnabled, "tls", "t", false, "enable tls connection")
	Cmd.Flags().StringVar(&hostAddr, "host", "localhost:8080", "host addr")
	Cmd.Flags().StringVar(&topicID, "topic", "life is beautiful", "topic id")
	Cmd.Flags().StringVarP(&nickname, "nickname", "n", fmt.Sprintf("alien-%d", r), "consumer id")

	Cmd.Flags().DurationVar(&interval, "interval", 1*time.Second, "interval")
	Cmd.Flags().IntVarP(&loadCount, "count", "c", 100, "load count")
}
