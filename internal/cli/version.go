package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Build metadata, overridden at build time with -ldflags.
var (
	Commit    = "none"
	BuildDate = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(),
				"ripple %s (commit %s, built %s, %s/%s)\n",
				Version, Commit, BuildDate, runtime.GOOS, runtime.GOARCH)
			return err
		},
	}
}
