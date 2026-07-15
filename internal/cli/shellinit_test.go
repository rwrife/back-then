package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestFindPrint0EmitsNulSeparatedAbsolutePaths verifies --print0 outputs each
// result as an absolute path terminated by a NUL byte (for xargs -0/fzf --read0)
// with no table header or trailing newline separators.
func TestFindPrint0EmitsNulSeparatedAbsolutePaths(t *testing.T) {
	db := seedIndex(t, map[string]time.Time{
		"a.txt": time.Now(),
		"b.txt": time.Now(),
	})
	out, err := executeDB(t, db, "find", "this month", "--print0")
	if err != nil {
		t.Fatalf("find --print0: %v", err)
	}
	if strings.Contains(out, "SCORE") || strings.Contains(out, "Window:") {
		t.Errorf("--print0 must not emit table chrome; got: %q", out)
	}
	if !strings.Contains(out, "\x00") {
		t.Errorf("--print0 must emit NUL separators; got: %q", out)
	}
	parts := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	if len(parts) == 0 {
		t.Fatal("expected at least one path")
	}
	for _, p := range parts {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			t.Errorf("path is not absolute: %q", p)
		}
		if strings.ContainsAny(p, "\n") {
			t.Errorf("path should not contain newlines under --print0: %q", p)
		}
	}
}

// TestFindPathsOnlyEmitsNewlinePaths verifies --paths-only emits bare newline
// separated paths without the table header.
func TestFindPathsOnlyEmitsNewlinePaths(t *testing.T) {
	db := seedIndex(t, map[string]time.Time{
		"a.txt": time.Now(),
	})
	out, err := executeDB(t, db, "find", "this month", "--paths-only")
	if err != nil {
		t.Fatalf("find --paths-only: %v", err)
	}
	if strings.Contains(out, "SCORE") || strings.Contains(out, "Window:") {
		t.Errorf("--paths-only must not emit table chrome; got: %q", out)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		if !filepath.IsAbs(line) {
			t.Errorf("path is not absolute: %q", line)
		}
	}
}

// TestShellInitSnippets checks each supported shell prints a sourceable snippet
// defining the bt alias and bt-cd helper, and that an unknown shell errors.
func TestShellInitSnippets(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		out, err := execute(t, "shell-init", sh)
		if err != nil {
			t.Fatalf("shell-init %s: %v", sh, err)
		}
		if !strings.Contains(out, "bt-cd") {
			t.Errorf("%s snippet missing bt-cd helper; got: %q", sh, out)
		}
		if !strings.Contains(out, "alias bt=") {
			t.Errorf("%s snippet missing bt alias; got: %q", sh, out)
		}
		if !strings.Contains(out, "--print0") {
			t.Errorf("%s snippet should pipe --print0 into fzf; got: %q", sh, out)
		}
	}
	if _, err := execute(t, "shell-init", "powershell"); err == nil {
		t.Error("expected error for unsupported shell, got nil")
	}
}
