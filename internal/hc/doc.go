package hc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// appDocName is the document the herdr desktop app keeps per machine:
// its spaces, tabs and pane splits (ADR-0011). Couch Pi reads it and
// never writes it: doc.set is newest-wins with no merge, so a write
// here could clobber the app's own edits.
const appDocName = "herdr-app"

// Space is one space from the herdr-app document. Name is empty while
// nobody named it; the app derives such names from the folder, so
// callers fold to the folder basename for display.
type Space struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
	// PinFolder is the folder from the space's own space-pin block in
	// its tabs. The app maintains the pin when a space is re-pointed,
	// while the folder field drifts (live doc, 2026-11-19), so Dir()
	// prefers the pin.
	PinFolder string `json:"-"`
	// FirstTab is the id of the doc's first tab in this space: the
	// herdr.tab adoption target for a phone spawn. Empty when the doc
	// lists no tab, and callers then spawn without labels rather than
	// guess an id the app does not know.
	FirstTab string `json:"-"`
}

// Dir is the folder this space owns: the pin folder when the doc
// carries the space's own pin, else the folder field.
func (s Space) Dir() string {
	if s.PinFolder != "" {
		return s.PinFolder
	}
	return s.Folder
}

// docTab is the slice of a doc tab Couch Pi needs.
type docTab struct {
	ID   string     `json:"id"`
	Root docTabRoot `json:"root"`
}

// docTabRoot carries the tab's pane blocks; a space-pin block names
// the folder the space is pinned to.
type docTabRoot struct {
	Children []docBlock `json:"children"`
}

type docBlock struct {
	Type    string `json:"type"`
	Content struct {
		Mod   string `json:"mod"`
		Props struct {
			Folder string `json:"folder"`
			Space  string `json:"space"`
		} `json:"props"`
	} `json:"content"`
}

// pinOwnsSpace matches a pin's space reference to a space id: the pin
// may carry a machine prefix ("machine/<id>") on a federated doc.
func pinOwnsSpace(spaceID, pinSpace string) bool {
	return pinSpace == spaceID || strings.HasSuffix(pinSpace, "/"+spaceID)
}

// DocGet reads one core document and its version. A name never
// written comes back as a nil value (version zero), not an error.
func (c *Client) DocGet(ctx context.Context, name string) (json.RawMessage, error) {
	res, err := c.call(ctx, "doc.get", map[string]any{"name": name}, 0)
	if err != nil {
		return nil, err
	}
	var out struct {
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("hc: decode doc.get %s: %w", name, err)
	}
	return out.Value, nil
}

// Spaces lists the spaces in the app document. Tolerant parse: a
// missing or malformed document degrades to an empty list, and one
// junk entry never sinks the rest.
func (c *Client) Spaces(ctx context.Context) ([]Space, error) {
	raw, err := c.DocGet(ctx, appDocName)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return []Space{}, nil
	}
	var doc struct {
		Spaces []json.RawMessage `json:"spaces"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return []Space{}, nil
	}
	out := make([]Space, 0, len(doc.Spaces))
	for _, rawSpace := range doc.Spaces {
		var s struct {
			Space
			Tabs []docTab `json:"tabs"`
		}
		if json.Unmarshal(rawSpace, &s) != nil || s.ID == "" {
			continue
		}
		if len(s.Tabs) > 0 {
			s.FirstTab = s.Tabs[0].ID
		}
		for _, tab := range s.Tabs {
			for _, b := range tab.Root.Children {
				if b.Content.Mod != "space-pin" || !pinOwnsSpace(s.ID, b.Content.Props.Space) {
					continue
				}
				if s.PinFolder == "" {
					s.PinFolder = b.Content.Props.Folder
				}
			}
		}
		out = append(out, s.Space)
	}
	return out, nil
}
