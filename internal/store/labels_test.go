package store

import (
	"testing"
	"time"
)

func TestAddAndListLabels(t *testing.T) {
	s := openTemp(t)

	start := time.Date(2024, 1, 15, 9, 30, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	if err := s.AddLabel("20240115-0930", "Berlin trip", start, end); err != nil {
		t.Fatalf("AddLabel: %v", err)
	}

	labels, err := s.Labels()
	if err != nil {
		t.Fatalf("Labels: %v", err)
	}
	if len(labels) != 1 {
		t.Fatalf("Labels len = %d, want 1", len(labels))
	}
	got := labels[0]
	if got.ID != "20240115-0930" || got.Label != "Berlin trip" {
		t.Errorf("label = %+v, want id 20240115-0930 / Berlin trip", got)
	}
	if !got.Start.Equal(start) || !got.End.Equal(end) {
		t.Errorf("window = [%s,%s], want [%s,%s]", got.Start, got.End, start, end)
	}
}

func TestAddLabelReplacesSameSession(t *testing.T) {
	s := openTemp(t)
	start := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	if err := s.AddLabel("20240301-1200", "old name", start, end); err != nil {
		t.Fatal(err)
	}
	if err := s.AddLabel("20240301-1200", "new name", start, end); err != nil {
		t.Fatal(err)
	}

	labels, err := s.Labels()
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 1 {
		t.Fatalf("Labels len = %d, want 1 (re-tag should replace)", len(labels))
	}
	if labels[0].Label != "new name" {
		t.Errorf("label = %q, want %q", labels[0].Label, "new name")
	}
}

func TestLabelByNameCaseInsensitive(t *testing.T) {
	s := openTemp(t)
	start := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	if err := s.AddLabel("20240601-0800", "Berlin Trip", start, start.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{"Berlin Trip", "berlin trip", "BERLIN TRIP"} {
		got, err := s.LabelByName(q)
		if err != nil {
			t.Fatalf("LabelByName(%q): %v", q, err)
		}
		if len(got) != 1 {
			t.Errorf("LabelByName(%q) len = %d, want 1", q, len(got))
		}
	}

	none, err := s.LabelByName("Paris trip")
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Errorf("LabelByName(unknown) len = %d, want 0", len(none))
	}
}

func TestAddLabelRejectsEmpty(t *testing.T) {
	s := openTemp(t)
	now := time.Now()
	if err := s.AddLabel("", "x", now, now); err == nil {
		t.Error("AddLabel with empty id should error")
	}
	if err := s.AddLabel("id", "", now, now); err == nil {
		t.Error("AddLabel with empty label should error")
	}
}
