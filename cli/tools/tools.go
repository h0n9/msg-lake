package tools

import (
	"github.com/h0n9/msg-lake/cli/tools/loader"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "tools",
	Short: "useful tools for msg-lake",
}

func init() {
	cobra.EnableCommandSorting = false
	Cmd.AddCommand(
		loader.Cmd,
	)
}
