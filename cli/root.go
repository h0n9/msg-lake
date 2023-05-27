package cli

import (
	"github.com/spf13/cobra"

	"github.com/h0n9/msg-lake/cli/agent"
	"github.com/h0n9/msg-lake/cli/client"
)

var RootCmd = &cobra.Command{
	Use:   "lake",
	Short: "simple msg lake",
}

func init() {
	cobra.EnableCommandSorting = false
	RootCmd.AddCommand(
		agent.Cmd,
		client.Cmd,
	)
}
