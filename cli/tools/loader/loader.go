package loader

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/spf13/cobra"
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

	intervalStr string // "1s"
	loadCount   int    // 0 means unlimited
)

func runE(cmd *cobra.Command, args []string) error {
	return nil
}

func init() {
	r := rand.New(rand.NewSource(time.Now().UnixMicro())).Int()

	Cmd.Flags().BoolVarP(&tlsEnabled, "tls", "t", false, "enable tls connection")
	Cmd.Flags().StringVar(&hostAddr, "host", "localhost:8080", "host addr")
	Cmd.Flags().StringVar(&topicID, "topic", "life is beautiful", "topic id")
	Cmd.Flags().StringVarP(&nickname, "nickname", "n", fmt.Sprintf("alien-%d", r), "consumer id")

	Cmd.Flags().StringVar(&intervalStr, "interval", "1s", "interval")
	Cmd.Flags().IntVarP(&loadCount, "count", "c", 100, "load count")
}
