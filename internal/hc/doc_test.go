package hc

import (
	"context"
	"encoding/json"
	"net"
	"testing"
)

// docGetHandler serves a canned doc.get for appDocName and records
// the requested names.
func docGetHandler(t *testing.T, docs map[string]any, names *[]string) func(string, json.RawMessage) (any, *Error) {
	t.Helper()
	return func(method string, params json.RawMessage) (any, *Error) {
		if method != "doc.get" {
			return nil, &Error{Code: -32601, Message: "method not found"}
		}
		var p struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(params, &p) != nil {
			t.Fatalf("doc.get params unparsable: %s", params)
		}
		*names = append(*names, p.Name)
		v, ok := docs[p.Name]
		if !ok {
			return map[string]any{"value": nil, "version": 0}, nil
		}
		return map[string]any{"value": v, "version": 21}, nil
	}
}

func TestSpacesReadsTheAppDocument(t *testing.T) {
	var names []string
	f := startFakeCore(t, docGetHandler(t, map[string]any{
		"herdr-app": map[string]any{
			"spaces": []any{
				map[string]any{"id": "sp1", "name": "", "folder": "/home/me/Dev/pi-couch", "tabs": []any{map[string]any{
					"id": "tb1",
					"root": map[string]any{"children": []any{
						map[string]any{"type": "block", "content": map[string]any{"mod": "space-pin", "props": map[string]any{"folder": "/elsewhere", "space": "sp999"}}},
						map[string]any{"type": "block", "content": map[string]any{"mod": "space-pin", "props": map[string]any{"folder": "/home/me/Dev", "space": "zenless/sp1"}}},
					}},
				}}},
				map[string]any{"id": "sp2", "name": "Forge", "folder": "/home/me/Dev/EstebanForge"},
				map[string]any{"folder": "/idless/is/skipped"},
				"junk-not-an-object",
			},
		},
	}, &names))
	c := NewAt(f.socket())

	spaces, err := c.Spaces(context.Background())
	if err != nil {
		t.Fatalf("Spaces: %v", err)
	}
	if len(names) != 1 || names[0] != "herdr-app" {
		t.Fatalf("doc.get names = %v", names)
	}
	if len(spaces) != 2 {
		t.Fatalf("spaces = %+v, want 2 (idless and junk skipped)", spaces)
	}
	if spaces[0].ID != "sp1" || spaces[0].Name != "" || spaces[0].Folder != "/home/me/Dev/pi-couch" {
		t.Fatalf("spaces[0] = %+v", spaces[0])
	}
	if spaces[0].FirstTab != "tb1" {
		t.Fatalf("spaces[0].FirstTab = %q, want tb1", spaces[0].FirstTab)
	}
	// The space's own pin wins over another space's pin, machine
	// prefixes in the pin's reference tolerated; a pinless space keeps
	// its folder field.
	if spaces[0].PinFolder != "/home/me/Dev" || spaces[0].Dir() != "/home/me/Dev" {
		t.Fatalf("spaces[0] pin = %q dir = %q, want the own pin", spaces[0].PinFolder, spaces[0].Dir())
	}
	if spaces[1].ID != "sp2" || spaces[1].Name != "Forge" || spaces[1].FirstTab != "" {
		t.Fatalf("spaces[1] = %+v", spaces[1])
	}
	if spaces[1].PinFolder != "" || spaces[1].Dir() != "/home/me/Dev/EstebanForge" {
		t.Fatalf("spaces[1] pin = %q dir = %q, want the folder field", spaces[1].PinFolder, spaces[1].Dir())
	}
}

// A core whose app document was never written (doc.get answers a nil
// value) lists no spaces and no error: the sheet just shows the
// folder browser.
func TestSpacesEmptyWhenDocNeverWritten(t *testing.T) {
	var names []string
	f := startFakeCore(t, docGetHandler(t, map[string]any{}, &names))
	c := NewAt(f.socket())

	spaces, err := c.Spaces(context.Background())
	if err != nil {
		t.Fatalf("Spaces: %v", err)
	}
	if len(spaces) != 0 {
		t.Fatalf("spaces = %+v, want empty", spaces)
	}
}

// A malformed app document degrades to an empty list: the doc belongs
// to the app, and a listing must never hard-fail on its content.
func TestSpacesToleratesMalformedDoc(t *testing.T) {
	var names []string
	f := startFakeCore(t, docGetHandler(t, map[string]any{
		"herdr-app": map[string]any{"spaces": "not-an-array"},
	}, &names))
	c := NewAt(f.socket())

	spaces, err := c.Spaces(context.Background())
	if err != nil {
		t.Fatalf("Spaces: %v", err)
	}
	if len(spaces) != 0 {
		t.Fatalf("spaces = %+v, want empty", spaces)
	}
}

// A dead socket is an error: the spaces listing is an action surface
// and fails loudly.
func TestSpacesFailsOnDeadSocket(t *testing.T) {
	ln, err := net.Listen("unix", t.TempDir()+"/dead.sock")
	if err != nil {
		t.Fatal(err)
	}
	path := ln.Addr().String()
	_ = ln.Close()
	c := NewAt(path)

	if _, err := c.Spaces(context.Background()); err == nil {
		t.Fatal("want error on dead socket")
	}
}
