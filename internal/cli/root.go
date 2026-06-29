// Package cli wires up the back-then command-line interface using cobra.
//
// The root command and its subcommands live here so that main() stays a thin
// entrypoint and the command tree is easy to unit-test.
package cli

import (
	"github.com/spf13/cobra"
)

// rootLong is the long description shown by `back-then --help`.
const rootLong = `back-then is a local-first time machine for your filesystem.

Instead of searching by a filename you don't remember, you search by roughly
when a file showed up and what was going on around it: "that spreadsheet from
around tax season," "the photo from the week of the move." It reads only
on-disk signals (timestamps, EXIF capture dates, the burst of files that
arrived together, the folder they lived in) and ranks candidates.

100% offline. No cloud, no account, no telemetry. Your files never leave the
machine.`

// NewRootCmd builds the root cobra command with all subcommands attached.
// It is a constructor (rather than a package-level var) so tests can build a
// fresh, isolated command tree per case.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "back-then",
		Short: "A local-first time machine for your files",
		Long:  rootLong,
		// We define our own subcommands; silence cobra's usage/error spew so
		// callers (main) control exit behavior.
		SilenceUsage:  true,
		SilenceErrors: true,
		// Running the bare command with no subcommand prints help rather than
		// erroring out.
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.AddCommand(newVersionCmd())

	return root
}
