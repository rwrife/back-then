package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rwrife/back-then/internal/store"
	"github.com/rwrife/back-then/internal/walk"
)

// fakeIndexer records each Index call and returns a scripted result/error. It
// lets the watch loop be tested deterministically without a real SQLite index
// or filesystem, and can cancel a supplied context after N passes so the
// long-lived loop terminates.
type fakeIndexer struct {
	mu     sync.Mutex
	calls  int
	result store.IndexResult
	err    error

	// stopAfter cancels cancel() once calls reaches this count (>0), used to
	// break out of the infinite loop from inside a pass.
	stopAfter int
	cancel    context.CancelFunc
}

func (f *fakeIndexer) Index(roots []string, opts walk.Options) (store.IndexResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.stopAfter > 0 && f.calls >= f.stopAfter && f.cancel != nil {
		f.cancel()
	}
	return f.result, f.err
}

func (f *fakeIndexer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestRunWatchOnceRunsSinglePass(t *testing.T) {
	idx := &fakeIndexer{result: store.IndexResult{Seen: 5, Upserted: 2, Skipped: 3}}
	var buf bytes.Buffer

	err := runWatch(context.Background(), idx, &buf, "/tmp/index.db", []string{"/data"}, walk.Options{}, time.Second, true)
	if err != nil {
		t.Fatalf("runWatch(once) returned error: %v", err)
	}
	if got := idx.count(); got != 1 {
		t.Errorf("expected exactly 1 index pass with --once, got %d", got)
	}

	out := buf.String()
	if !strings.Contains(out, "5 seen, 2 updated, 3 unchanged") {
		t.Errorf("summary line missing expected counts; got: %q", out)
	}
	// --once should not print the "Watching ..." banner or stop notice.
	if strings.Contains(out, "Watching") || strings.Contains(out, "Stopped watching") {
		t.Errorf("--once output should not include loop banners; got: %q", out)
	}
}

func TestRunWatchOncePropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	idx := &fakeIndexer{err: wantErr}
	var buf bytes.Buffer

	err := runWatch(context.Background(), idx, &buf, "/tmp/index.db", []string{"/data"}, walk.Options{}, time.Second, true)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the first-pass error to propagate, got %v", err)
	}
}

func TestRunWatchLoopsUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Stop the loop from inside the third pass so we exercise the immediate
	// pass plus two ticker-driven passes without relying on wall-clock timing.
	idx := &fakeIndexer{
		result:    store.IndexResult{Seen: 1},
		stopAfter: 3,
		cancel:    cancel,
	}
	var buf bytes.Buffer

	done := make(chan error, 1)
	go func() {
		// Tiny interval keeps the test fast; the floor is enforced at the CLI
		// layer, not in runWatch, so a sub-second value is fine here.
		done <- runWatch(ctx, idx, &buf, "/tmp/index.db", []string{"/data"}, walk.Options{}, 5*time.Millisecond, false)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWatch returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runWatch did not terminate after context cancellation")
	}

	if got := idx.count(); got < 3 {
		t.Errorf("expected at least 3 passes before cancel, got %d", got)
	}
	out := buf.String()
	if !strings.Contains(out, "Watching 1 path(s)") {
		t.Errorf("expected watching banner; got: %q", out)
	}
	if !strings.Contains(out, "Stopped watching") {
		t.Errorf("expected stop notice on cancellation; got: %q", out)
	}
}

func TestRunWatchSurvivesTransientScanError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First pass succeeds; a later pass errors but must not tear down the
	// loop. We cancel on the 3rd call to end the test.
	idx := &erroringIndexer{failOn: 2, stopOn: 3, cancel: cancel, ok: store.IndexResult{Seen: 1}}
	var buf bytes.Buffer

	done := make(chan error, 1)
	go func() {
		done <- runWatch(ctx, idx, &buf, "/tmp/index.db", []string{"/data"}, walk.Options{}, 5*time.Millisecond, false)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runWatch should swallow transient scan errors, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runWatch did not terminate after context cancellation")
	}

	if !strings.Contains(buf.String(), "watch: scan error: transient") {
		t.Errorf("expected transient scan error to be reported; got: %q", buf.String())
	}
}

// erroringIndexer returns an error on a specific pass number and cancels the
// loop on another, letting us assert the loop keeps running past a failure.
type erroringIndexer struct {
	mu     sync.Mutex
	calls  int
	failOn int
	stopOn int
	cancel context.CancelFunc
	ok     store.IndexResult
}

func (e *erroringIndexer) Index(roots []string, opts walk.Options) (store.IndexResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls++
	if e.stopOn > 0 && e.calls >= e.stopOn && e.cancel != nil {
		e.cancel()
	}
	if e.calls == e.failOn {
		return store.IndexResult{}, errors.New("transient")
	}
	return e.ok, nil
}

func TestWatchCommandRejectsMissingPath(t *testing.T) {
	// A non-existent root should fail fast before any indexing happens.
	if _, err := execute(t, "watch", "/nope/definitely/not/here", "--once"); err == nil {
		t.Error("expected error for a non-existent watch path, got nil")
	}
}

func TestRootHelpListsWatch(t *testing.T) {
	out, err := execute(t, "--help")
	if err != nil {
		t.Fatalf("--help returned error: %v", err)
	}
	if !strings.Contains(out, "watch") {
		t.Errorf("--help output does not list the watch command; got: %q", out)
	}
}
