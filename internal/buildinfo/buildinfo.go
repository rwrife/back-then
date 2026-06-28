// Package buildinfo holds version metadata for the back-then binary.
//
// These values are overridable at build time via -ldflags, e.g.:
//
//	go build -ldflags "\
//	  -X github.com/rwrife/back-then/internal/buildinfo.Version=v0.1.0 \
//	  -X github.com/rwrife/back-then/internal/buildinfo.Commit=$(git rev-parse --short HEAD) \
//	  -X github.com/rwrife/back-then/internal/buildinfo.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// When built without ldflags (e.g. `go run`, `go build ./...`), the defaults
// below are used so the binary still reports something sensible.
package buildinfo

var (
	// Version is the semantic version, or "dev" for unstamped builds.
	Version = "dev"
	// Commit is the short git SHA the binary was built from.
	Commit = "none"
	// Date is the build timestamp (UTC, RFC3339) when stamped.
	Date = "unknown"
)
