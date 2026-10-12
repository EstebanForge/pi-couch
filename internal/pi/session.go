// Package pi reads pi session JSONL trees. Tolerant by design: unknown
// entry types and malformed lines never fail a parse.
package pi

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// Header is the first line of a session file. It is metadata only and is not
// part of the entry tree.
type Header struct {
	Type          string `json:"type"`
	Version       int    `json:"version"`
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Cwd           string `json:"cwd"`
	ParentSession string `json:"parentSession,omitempty"`
}

// Entry is one JSONL line. Fields cover the entry types Couch Pi renders;
// unknown types decode with whatever fields they carry and are skipped.
type Entry struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	ParentID  *string `json:"parentId"`
	Timestamp string  `json:"timestamp"`

	Message *Message `json:"message,omitempty"`

	CustomType       string `json:"customType,omitempty"`
	Name             string `json:"name,omitempty"`     // session_info
	Summary          string `json:"summary,omitempty"`  // compaction, branch_summary
	Provider         string `json:"provider,omitempty"` // model_change
	ModelID          string `json:"modelId,omitempty"`  // model_change
	ThinkingLevel    string `json:"thinkingLevel,omitempty"`
	Kind             string `json:"kind,omitempty"` // usage
	Usage            *Usage `json:"usage,omitempty"`
	FirstKeptEntryID string `json:"firstKeptEntryId,omitempty"`
	TokensBefore     int64  `json:"tokensBefore,omitempty"`
}

// Message wraps AgentMessage. Content arrives either as a plain string
// (user) or as a block array (assistant, toolResult); decode on demand.
type Message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content,omitempty"`
	Model      string          `json:"model,omitempty"`
	Provider   string          `json:"provider,omitempty"`
	StopReason string          `json:"stopReason,omitempty"`
	Usage      *Usage          `json:"usage,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	IsError    bool            `json:"isError,omitempty"`
	DurationMs int64           `json:"durationMs,omitempty"`
	Timestamp  int64           `json:"timestamp,omitempty"`
}

// Usage is the token/cost record on assistant messages and usage entries.
type Usage struct {
	Input       int64 `json:"input"`
	Output      int64 `json:"output"`
	TotalTokens int64 `json:"totalTokens"`
	Cost        struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

// Block is one content block: text, image, thinking, or toolCall.
type Block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Redacted  bool            `json:"redacted,omitempty"`
	ID        string          `json:"id,omitempty"`   // toolCall
	Name      string          `json:"name,omitempty"` // toolCall
	Arguments json.RawMessage `json:"arguments,omitempty"`
	MimeType  string          `json:"mimeType,omitempty"`
	Data      string          `json:"data,omitempty"`
}

// Blocks decodes message content into blocks. A string content becomes one
// text block. Unknown block types keep their raw fields.
func (m *Message) Blocks() []Block {
	if m == nil || len(m.Content) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(m.Content, &s); err == nil {
		if s == "" {
			return nil
		}
		return []Block{{Type: "text", Text: s}}
	}
	var blocks []Block
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return nil
	}
	return blocks
}

// ParseHeader reads the session header from the first parseable line.
func ParseHeader(r io.Reader) (Header, error) {
	var h Header
	found := false
	parseLines(r, func(line string) {
		if found {
			return
		}
		var x Header
		if json.Unmarshal([]byte(line), &x) == nil && x.Type == "session" {
			h, found = x, true
		}
	})
	if !found {
		return Header{}, io.ErrUnexpectedEOF
	}
	return h, nil
}

// ParseEntries decodes every parseable line. Malformed lines are skipped:
// a torn final line from a live writer must not fail the read.
func ParseEntries(r io.Reader) []Entry {
	var out []Entry
	parseLines(r, func(line string) {
		var e Entry
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	})
	return out
}

// parseLines yields every non-empty line with no size cap. bufio.Scanner
// dies on ErrTooLong and silently drops the rest of the file, so one huge
// entry (a base64 image) would truncate the whole session.
func parseLines(r io.Reader, fn func(line string)) {
	rd := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := rd.ReadString('\n')
		if s := strings.TrimSpace(line); s != "" {
			fn(s)
		}
		if err != nil {
			return
		}
	}
}

// ActivePath resolves the current branch: childless entries are leaves; the
// last appended leaf wins. The path runs root to leaf.
func ActivePath(entries []Entry) []Entry {
	byID := make(map[string]Entry, len(entries))
	hasChild := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.ID != "" {
			byID[e.ID] = e
		}
		if e.ParentID != nil {
			hasChild[*e.ParentID] = true
		}
	}
	var leaf *Entry
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.ID == "" || hasChild[e.ID] {
			continue
		}
		leaf = &entries[i]
		break
	}
	if leaf == nil {
		return nil
	}
	var path []Entry
	visited := map[string]bool{}
	for e := *leaf; ; {
		if visited[e.ID] {
			return nil // malformed cycle in the parent chain
		}
		visited[e.ID] = true
		path = append(path, e)
		if e.ParentID == nil || *e.ParentID == "" {
			break
		}
		parent, ok := byID[*e.ParentID]
		if !ok {
			break
		}
		e = parent
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}
