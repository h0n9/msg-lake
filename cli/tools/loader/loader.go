package loader

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/h0n9/msg-lake/cli/identity"
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
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// init ctx with cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	/////////////////////////////////
	// real things begin from here //
	/////////////////////////////////

	// init privKey
	privKey, err := identity.NewDemoKey()
	if err != nil {
		return err
	}
	// init msg lake client
	msgLakeClient, err = client.NewClient(privKey, hostAddr, tlsEnabled)
	if err != nil {
		return err
	}
	defer msgLakeClient.Close()
	fmt.Printf("loader <%s> started\n", nickname)

	// listen signals
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
			return
		case s := <-sigCh:
			fmt.Printf("got signal %v, cancelling loader\n", s)
			cancel()
			msgLakeClient.Close()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		// init ticker with interval
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var (
			i   int = 0
			err error
		)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if loadCount > 0 && i >= loadCount {
					fmt.Println("end of load test")
					cancel()
					return
				}
				fmt.Printf("loader <%s>: %d\n", nickname, i)
				err = msgLakeClient.Publish(ctx, topicID, strconv.Itoa(i))
				if err != nil {
					fmt.Println(err)
				}

				// increment i by 1
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
	Cmd.Flags().StringVarP(&nickname, "nickname", "n", fmt.Sprintf("alien-%d", r), "display nickname")

	Cmd.Flags().DurationVar(&interval, "interval", 1*time.Second, "interval")
	Cmd.Flags().IntVarP(&loadCount, "count", "c", 0, "load count (0 means unlimited)")
}
