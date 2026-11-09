package hc

import "errors"

// Agent is the driver-neutral shape the awaiting detector, the push
// watcher and the create flow consume (formerly herdr.Agent). Session
// carries the pi session file path once pi's bridge labels it.
type Agent struct {
	Name          string
	AgentStatus   string
	PaneID        string
	WorkspaceID   string
	TerminalTitle string
	Session       *AgentSessionRef
}

// AgentSessionRef carries the pi session file path.
type AgentSessionRef struct {
	Kind  string
	Value string
}

// DrivenInfo is what the phone shows about the agent driving a session.
type DrivenInfo struct {
	Status string `json:"status"`
	Title  string `json:"title,omitempty"`
	Label  string `json:"label,omitempty"`
}

// PaneRead is one pane text read: plain text plus the pane revision.
type PaneRead struct {
	PaneID    string `json:"pane_id"`
	Source    string `json:"source"`
	Format    string `json:"format"`
	Text      string `json:"text"`
	Revision  int64  `json:"revision"`
	Truncated bool   `json:"truncated"`
}

// ErrNotDriven means no live terminal currently runs that session file.
var ErrNotDriven = errors.New("hc: no agent runs this session")
