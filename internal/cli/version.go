package cli

import (
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/rwrife/back-then/internal/buildinfo"
)

// newVersionCmd returns the `back-then version` subcommand.
//
// It supports a --short flag for scripting (prints just the version string)
// and otherwise prints a human-readable line including commit, build date,
// and the Go/OS/arch it was built for.
func newVersionCmd() *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the back-then version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if short {
				_, err := io.WriteString(out, buildinfo.Version+"\n")
				return err
			}
			_, err := fmt.Fprintf(
				out,
				"back-then %s (commit %s, built %s, %s %s/%s)\n",
				buildinfo.Version,
				buildinfo.Commit,
				buildinfo.Date,
				runtime.Version(),
				runtime.GOOS,
				runtime.GOARCH,
			)
			return err
		},
	}

	cmd.Flags().BoolVar(&short, "short", false, "print only the version string")

	return cmd
}
