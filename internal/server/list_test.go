package server

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/EstebanForge/couch-pi/internal/pi"
)

func mkSessions(paths ...string) []pi.SessionMeta {
	out := make([]pi.SessionMeta, len(paths))
	for i, p := range paths {
		out[i] = pi.SessionMeta{Key: "k", Path: p}
	}
	return out
}

func TestListWindow(t *testing.T) {
	paths := make([]string, 40)
	for i := range paths {
		paths[i] = fmt.Sprintf("/s%02d", i)
	}
	all := mkSessions(paths...)

	r := httptest.NewRequest("GET", "/", nil)
	got, w, total := listWindow(r, all, nil)
	if w != 15 || total != 40 || len(got) != 15 {
		t.Fatalf("default: w=%d total=%d len=%d", w, total, len(got))
	}

	r = httptest.NewRequest("GET", "/?w=25", nil)
	got, w, _ = listWindow(r, all, nil)
	if w != 25 || len(got) != 25 {
		t.Fatalf("w=25: w=%d len=%d", w, len(got))
	}

	r = httptest.NewRequest("GET", "/?w=garbage", nil)
	if _, w, _ = listWindow(r, all, nil); w != 15 {
		t.Fatalf("garbage: w=%d", w)
	}

	r = httptest.NewRequest("GET", "/?w=-3", nil)
	if _, w, _ = listWindow(r, all, nil); w != 15 {
		t.Fatalf("negative: w=%d", w)
	}

	r = httptest.NewRequest("GET", "/?w=2", nil)
	got, w, _ = listWindow(r, all, nil)
	if w != 15 || len(got) != 15 {
		t.Fatalf("below window: w=%d len=%d", w, len(got))
	}
}

func TestListWindowGroupsActiveFirst(t *testing.T) {
	// mtime order: newest first. One working and one blocked session sit
	// further down the recency list; they must bubble above the divider.
	all := mkSessions("/old1", "/work", "/old2", "/blk")
	badges := map[string]string{"/work": "working", "/blk": "blocked"}

	got, _, _ := listWindow(httptest.NewRequest("GET", "/", nil), all, badges)
	want := []string{"/work", "/blk", "/old1", "/old2"}
	for i, p := range want {
		if got[i].Path != p {
			t.Fatalf("order[%d]=%s want %s", i, got[i].Path, p)
		}
	}

	// Empty badge map keeps recency order untouched.
	got, _, _ = listWindow(httptest.NewRequest("GET", "/", nil), all, map[string]string{})
	if got[0].Path != "/old1" || got[3].Path != "/blk" {
		t.Fatalf("no badges: first=%s last=%s", got[0].Path, got[3].Path)
	}
}
