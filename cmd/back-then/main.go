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
		// cobra already prints the error; just set a non-zero exit code.
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}
}
