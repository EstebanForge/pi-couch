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
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var h Header
		if err := json.Unmarshal([]byte(line), &h); err != nil {
			continue
		}
		if h.Type != "session" {
			continue
		}
		return h, nil
	}
	return Header{}, io.ErrUnexpectedEOF
}

// ParseEntries decodes every parseable line. Malformed lines are skipped:
// a torn final line from a live writer must not fail the read.
func ParseEntries(r io.Reader) []Entry {
	var out []Entry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out
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
	for e := *leaf; ; {
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
