package hc

import (
	"context"
	"encoding/json"
	"fmt"
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
	// FirstTab is the id of the doc's first tab in this space: the
	// herdr.tab adoption target for a phone spawn. Empty when the doc
	// lists no tab, and callers then spawn without labels rather than
	// guess an id the app does not know.
	FirstTab string `json:"-"`
}

// docTab is the slice of a doc tab Couch Pi needs.
type docTab struct {
	ID string `json:"id"`
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
		out = append(out, s.Space)
	}
	return out, nil
}
