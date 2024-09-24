package loader

import (
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "loader",
	Short: "tool for load test",
	RunE:  runE,
}

func runE(cmd *cobra.Command, args []string) error {
	return nil
}

func init() {
	cobra.EnableCommandSorting = false
	Cmd.AddCommand()
}
