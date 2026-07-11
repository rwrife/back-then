package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// sessionIDFromJSON indexes the burst fixture and returns the id of the
// first-listed (newest) session, using the sessions --json output.
func firstSessionID(t *testing.T, db string) string {
	t.Helper()
	out, err := executeDB(t, db, "sessions", "--json")
	if err != nil {
		t.Fatalf("sessions --json error: %v\n%s", err, out)
	}
	var arr []struct {
		ID    string `json:"id"`
		Label string `json:"label"`
	}
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("unmarshal sessions json: %v\n%s", err, out)
	}
	if len(arr) == 0 {
		t.Fatal("no sessions in fixture")
	}
	if arr[0].ID == "" {
		t.Fatalf("session id empty in json: %s", out)
	}
	return arr[0].ID
}

func TestTagLabelsSession(t *testing.T) {
	db, _ := indexBurst(t)
	id := firstSessionID(t, db)

	out, err := executeDB(t, db, "tag", id, "Berlin trip")
	if err != nil {
		t.Fatalf("tag error: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Berlin trip") {
		t.Errorf("tag output missing label; got: %q", out)
	}

	// Label should now appear in sessions output.
	sout, err := executeDB(t, db, "sessions")
	if err != nil {
		t.Fatalf("sessions error: %v\n%s", err, sout)
	}
	if !strings.Contains(sout, "LABEL") {
		t.Errorf("sessions table missing LABEL header; got: %q", sout)
	}
	if !strings.Contains(sout, "Berlin trip") {
		t.Errorf("sessions table missing tagged label; got: %q", sout)
	}
}

func TestTagUnknownSession(t *testing.T) {
	db, _ := indexBurst(t)
	_, err := executeDB(t, db, "tag", "19990101-0000", "Nope")
	if err == nil {
		t.Fatal("tag with unknown session id should error")
	}
}

func TestFindMatchesLabel(t *testing.T) {
	db, _ := indexBurst(t)
	id := firstSessionID(t, db)

	if out, err := executeDB(t, db, "tag", id, "Berlin trip"); err != nil {
		t.Fatalf("tag error: %v\n%s", err, out)
	}

	// find by label (not a time phrase) should resolve to the tagged window
	// and return files from that burst.
	out, err := executeDB(t, db, "find", "Berlin trip")
	if err != nil {
		t.Fatalf("find by label error: %v\n%s", err, out)
	}
	if !strings.Contains(out, ".jpg") {
		t.Errorf("find by label missing burst files; got: %q", out)
	}
}

func TestFindUnknownLabelStillErrors(t *testing.T) {
	db, _ := indexBurst(t)
	// A phrase that is neither a time phrase nor a known label should still
	// surface the parse error rather than silently returning nothing.
	_, err := executeDB(t, db, "find", "not a real label or date")
	if err == nil {
		t.Fatal("find with unparseable, unlabeled query should error")
	}
}
