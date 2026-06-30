// Command back-then is a local-first time machine for your filesystem.
//
// You don't remember the filename — you remember roughly when a file showed
// up and what was going on around it. back-then searches by time and
// circumstance, fully offline. See PLAN.md for the roadmap.
package main

import (
	"fmt"
	"os"

	"github.com/rwrife/back-then/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		// The root command silences cobra's own error printing so we control
		// output; surface the error here and exit non-zero.
		fmt.Fprintln(os.Stderr, "back-then:", err)
		os.Exit(1)
	}
}
