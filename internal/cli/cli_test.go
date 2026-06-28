package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rwrife/back-then/internal/buildinfo"
)

// execute runs the root command with the given args, capturing stdout/stderr
// into a single buffer, and returns the combined output plus any error.
func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()

	root := NewRootCmd()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)

	err := root.Execute()
	return buf.String(), err
}

func TestVersionDefault(t *testing.T) {
	out, err := execute(t, "version")
	if err != nil {
		t.Fatalf("version returned error: %v", err)
	}
	if !strings.Contains(out, "back-then "+buildinfo.Version) {
		t.Errorf("version output missing version string %q; got: %q", buildinfo.Version, out)
	}
	if !strings.Contains(out, buildinfo.Commit) {
		t.Errorf("version output missing commit %q; got: %q", buildinfo.Commit, out)
	}
}

func TestVersionShort(t *testing.T) {
	out, err := execute(t, "version", "--short")
	if err != nil {
		t.Fatalf("version --short returned error: %v", err)
	}
	got := strings.TrimSpace(out)
	if got != buildinfo.Version {
		t.Errorf("version --short = %q, want %q", got, buildinfo.Version)
	}
}

func TestVersionRejectsArgs(t *testing.T) {
	if _, err := execute(t, "version", "extra"); err == nil {
		t.Error("expected error when passing positional args to version, got nil")
	}
}

func TestRootHelpListsVersion(t *testing.T) {
	out, err := execute(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	if !strings.Contains(out, "version") {
		t.Errorf("--help output does not list the version command; got: %q", out)
	}
	if !strings.Contains(out, "local-first") {
		t.Errorf("--help output missing long description; got: %q", out)
	}
}

func TestUnknownCommandErrors(t *testing.T) {
	if _, err := execute(t, "definitely-not-a-command"); err == nil {
		t.Error("expected error for unknown command, got nil")
	}
}
