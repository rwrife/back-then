//go:build windows

package cli

import "os"

// stopSignals returns the OS signals that should stop `back-then watch`.
// Windows has no SIGTERM, so we rely on os.Interrupt (Ctrl-C / Ctrl-Break),
// which the Go runtime maps from the console control events.
func stopSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
