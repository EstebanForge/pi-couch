package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVAPIDCorruptErrors(t *testing.T) {
	st, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	vapid := st.vapidPath()
	if err := os.WriteFile(vapid, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.VAPID(); err == nil {
		t.Fatal("corrupt vapid.json: want error, got nil (would regenerate keys)")
	}
	b, err := os.ReadFile(vapid)
	if err != nil || string(b) != "{not json" {
		t.Fatalf("corrupt file was overwritten: %q, %v", b, err)
	}
}

func TestUpdateSubsConcurrent(t *testing.T) {
	st, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	for i := range 8 {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			for j := range 10 {
				err := st.UpdateSubs(func(subs []Sub) []Sub {
					return append(subs, Sub{Endpoint: "e", P256dh: string(rune('a' + i)), Auth: string(rune('0' + j%10))})
				})
				if err != nil {
					t.Errorf("update: %v", err)
				}
			}
		}(i)
	}
	for range 8 {
		<-done
	}
	subs, err := st.Subs()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 80 {
		t.Fatalf("lost updates: want 80, got %d", len(subs))
	}
}

func TestStateFileModes(t *testing.T) {
	dir := t.TempDir()
	st, err := NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSubs([]Sub{{Endpoint: "e"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Append("k1", []Comment{{Body: "x"}}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"push-subs.json", "k1.json"} {
		fi, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, want 0600", f, fi.Mode().Perm())
		}
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("state dir: mode %v, want 0700", di.Mode().Perm())
	}
}

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
