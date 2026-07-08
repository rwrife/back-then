//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// stopSignals returns the OS signals that should stop `back-then watch`. On
// Unix-likes that's Ctrl-C (SIGINT) plus SIGTERM (e.g. from a service manager
// or `kill`), so the watcher shuts down cleanly under either.
func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
