package store

import "testing"

func TestSubsEmptyIsNotError(t *testing.T) {
	st, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	subs, err := st.Subs()
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 0 {
		t.Fatalf("want 0 subs, got %d", len(subs))
	}
}

func TestSubsRoundTrip(t *testing.T) {
	st, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	in := []Sub{{Endpoint: "https://push.example/abc", P256dh: "k1", Auth: "a1"}}
	if err := st.SetSubs(in); err != nil {
		t.Fatal(err)
	}
	out, err := st.Subs()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Endpoint != in[0].Endpoint || out[0].P256dh != "k1" || out[0].Auth != "a1" {
		t.Fatalf("roundtrip mismatch: %+v", out)
	}
}

func TestSubsReplace(t *testing.T) {
	st, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSubs([]Sub{{Endpoint: "e1"}, {Endpoint: "e2"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSubs([]Sub{{Endpoint: "e3"}}); err != nil {
		t.Fatal(err)
	}
	out, _ := st.Subs()
	if len(out) != 1 || out[0].Endpoint != "e3" {
		t.Fatalf("want replace not merge: %+v", out)
	}
}

func TestSubsConcurrent(t *testing.T) {
	st, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	for i := range 4 {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			for j := range 20 {
				_ = st.SetSubs([]Sub{{Endpoint: "e", P256dh: string(rune('a' + i))}})
				if _, err := st.Subs(); err != nil {
					t.Errorf("read: %v", err)
					_ = j
				}
			}
		}(i)
	}
	for range 4 {
		<-done
	}
}

func TestPushKeyGeneratesOnce(t *testing.T) {
	st, err := NewDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k1, err := st.VAPID()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := st.VAPID()
	if err != nil {
		t.Fatal(err)
	}
	if k1.Public != k2.Public || k1.Private == "" {
		t.Fatalf("keys not stable: %q vs %q", k1.Public, k2.Public)
	}
}
