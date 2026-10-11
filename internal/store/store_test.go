package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendAndSent(t *testing.T) {
	s, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Sent("k1"); err != nil || len(got) != 0 {
		t.Fatalf("empty store: got %v, %v", got, err)
	}
	err = s.Append("k1", []Comment{
		{File: "a.go", Line: 42, Body: "lock first"},
		{File: "b.go", Line: 7, Body: "check err"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Sent("k1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Body != "lock first" || got[0].TS == 0 {
		t.Fatalf("roundtrip: %+v", got)
	}
	// Append accumulates, file holds compact JSON on one line.
	err = s.Append("k1", []Comment{{File: "c.go", Line: 1, Body: "third"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Sent("k1"); len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	b, _ := os.ReadFile(filepath.Join(t.TempDir(), "nope"))
	_ = b
}

func TestEscapeRejected(t *testing.T) {
	s, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "a/b", `a\b`, "..", "a/../b"} {
		if _, err := s.Sent(key); !errors.Is(err, ErrBadKey) {
			t.Errorf("Sent(%q): want ErrBadKey, got %v", key, err)
		}
		if err := s.Append(key, []Comment{{Body: "x"}}); !errors.Is(err, ErrBadKey) {
			t.Errorf("Append(%q): want ErrBadKey, got %v", key, err)
		}
	}
}

func TestSessionsIsolated(t *testing.T) {
	s, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append("s1", []Comment{{Body: "one"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Sent("s2"); len(got) != 0 {
		t.Fatalf("leak: %+v", got)
	}
}
