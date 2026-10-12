package pi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SessionMeta is the list-page projection of one session file.
type SessionMeta struct {
	Key         string
	Path        string
	ID          string
	Cwd         string
	Name        string
	FirstUser   string
	Mtime       time.Time
	Version     int
	Entries     int
	TotalTokens int64
	TotalCost   float64
	Model       string
}

// DisplayName: session_info name, else first user text, else cwd base.
func (m SessionMeta) DisplayName() string {
	if m.Name != "" {
		return m.Name
	}
	if m.FirstUser != "" {
		return m.FirstUser
	}
	return filepath.Base(m.Cwd)
}

// Index walks the sessions root and caches list metadata per file.
// Safe for concurrent use: SSE streams and page handlers share one Index.
type Index struct {
	root  string
	mu    sync.Mutex
	cache map[string]metaCache
	tail  *tailCache
}

// tailCache holds the parsed entries of the session currently being
// viewed so transcript streams parse only appended bytes. One session
// at a time; a full parse of a large active session per watcher tick
// starved both the server and its clients.
type tailCache struct {
	key     string
	path    string
	offset  int64 // bytes consumed so far, always at a newline boundary
	entries []Entry
}

type metaCache struct {
	mtime time.Time
	size  int64
	meta  SessionMeta
}

func NewIndex(root string) *Index {
	return &Index{root: root, cache: map[string]metaCache{}}
}

// List returns every session file's metadata, newest first.
func (ix *Index) List() ([]SessionMeta, error) {
	dirs, err := os.ReadDir(ix.root)
	if err != nil {
		return nil, err
	}
	var out []SessionMeta
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(ix.root, d.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			path := filepath.Join(ix.root, d.Name(), f.Name())
			meta, ok := ix.cachedMeta(path)
			if ok {
				out = append(out, meta)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Mtime.After(out[j].Mtime) })
	return out, nil
}

// Meta returns one session by URL key (file name without .jsonl).
func (ix *Index) Meta(key string) (SessionMeta, bool) {
	if !validKey(key) {
		return SessionMeta{}, false
	}
	for _, d := range ix.subdirs() {
		path := filepath.Join(ix.root, d, key+".jsonl")
		if meta, ok := ix.cachedMeta(path); ok {
			return meta, true
		}
	}
	return SessionMeta{}, false
}

// validKey rejects keys that could escape the sessions root, mirroring the
// store's key rules: keys are URL path values, not trusted strings.
func validKey(key string) bool {
	return key != "" && !strings.ContainsAny(key, "/\\") && !strings.Contains(key, "..")
}

// ByPath returns one session by absolute file path (herdr reports the
// session file path; push notifications deep-link back into the UI).
func (ix *Index) ByPath(path string) (SessionMeta, bool) {
	meta, ok := ix.cachedMeta(path)
	return meta, ok
}

func (ix *Index) subdirs() []string {
	dirs, err := os.ReadDir(ix.root)
	if err != nil {
		return nil
	}
	var out []string
	for _, d := range dirs {
		if d.IsDir() {
			out = append(out, d.Name())
		}
	}
	return out
}

// cachedMeta serves from the cache on a hit; on a miss it parses WITHOUT
// holding the lock, so one slow multi-MB parse never head-of-line blocks
// every other stream. Concurrent misses may parse the same file twice; the
// last write to the cache wins and both results are identical.
func (ix *Index) cachedMeta(path string) (SessionMeta, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return SessionMeta{}, false
	}
	key := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	ix.mu.Lock()
	c, ok := ix.cache[key]
	ix.mu.Unlock()
	if ok && c.mtime.Equal(st.ModTime()) && c.size == st.Size() {
		return c.meta, true
	}
	meta, ok := ix.parse(path, key, st)
	if !ok {
		return SessionMeta{}, false
	}
	ix.mu.Lock()
	ix.cache[key] = metaCache{mtime: st.ModTime(), size: st.Size(), meta: meta}
	ix.mu.Unlock()
	return meta, true
}

func (ix *Index) parse(path, key string, st os.FileInfo) (SessionMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return SessionMeta{}, false
	}
	defer f.Close()
	h, err := ParseHeader(f)
	if err != nil {
		return SessionMeta{}, false
	}
	if _, err := f.Seek(0, 0); err != nil {
		return SessionMeta{}, false
	}
	entries := ParseEntries(f)

	meta := SessionMeta{
		Key:     key,
		Path:    path,
		ID:      h.ID,
		Cwd:     h.Cwd,
		Mtime:   st.ModTime(),
		Version: h.Version,
		Entries: len(entries),
	}
	for _, e := range entries {
		if u := e.Usage; u != nil {
			meta.TotalTokens += u.TotalTokens
			meta.TotalCost += u.Cost.Total
		}
		switch e.Type {
		case "session_info":
			if e.Name != "" {
				meta.Name = e.Name
			}
		case "model_change":
			if e.ModelID != "" {
				meta.Model = e.Provider + "/" + e.ModelID
			}
		case "message":
			m := e.Message
			if m == nil {
				continue
			}
			if m.Usage != nil {
				meta.TotalTokens += m.Usage.TotalTokens
				meta.TotalCost += m.Usage.Cost.Total
			}
			if m.Model != "" {
				meta.Model = m.Provider + "/" + m.Model
			}
			if meta.FirstUser == "" && m.Role == "user" {
				meta.FirstUser = firstText(m)
			}
		}
	}
	meta.FirstUser = truncate(meta.FirstUser, 80)
	return meta, true
}

// ViewItem is one renderable transcript row.
type ViewItem struct {
	Kind       string // user, text, thinking, tool_call, tool_result, marker, bash, custom
	Text       string
	ToolName   string
	IsError    bool
	Redacted   bool
	DurationMs int64
	Images     int
}

// Transcript renders the tail of a session's active branch.
func (ix *Index) Transcript(key string, limit int) (SessionMeta, []ViewItem, error) {
	meta, ok := ix.Meta(key)
	if !ok {
		return meta, nil, fmt.Errorf("session %q not found", key)
	}
	st, err := os.Stat(meta.Path)
	if err != nil {
		return meta, nil, err
	}
	entries, err := ix.tailEntries(key, meta.Path, st.Size())
	if err != nil {
		return meta, nil, err
	}
	path := ActivePath(entries)
	if limit > 0 && len(path) > limit {
		path = path[len(path)-limit:]
	}
	items := make([]ViewItem, 0, len(path))
	for _, e := range path {
		switch {
		case e.Message != nil:
			items = append(items, messageItems(e)...)
		case e.Type == "compaction":
			items = append(items, ViewItem{Kind: "marker", Text: fmt.Sprintf("Context compacted (%d tokens before)", e.TokensBefore)})
		case e.Type == "model_change":
			items = append(items, ViewItem{Kind: "marker", Text: "Model -> " + e.Provider + "/" + e.ModelID})
		case e.Type == "branch_summary":
			items = append(items, ViewItem{Kind: "marker", Text: "Branch summary: " + truncate(e.Summary, 140)})
		case e.Type == "custom_message":
			items = append(items, ViewItem{Kind: "custom", Text: "custom: " + truncate(e.CustomType, 40)})
		}
	}
	return meta, items, nil
}

// tailEntries returns the session's entries, parsing only the bytes
// appended since the previous call for this session. The offset always
// sits on a newline boundary, so a line the writer has not finished is
// picked up once it is complete, never duplicated.
func (ix *Index) tailEntries(key, path string, size int64) ([]Entry, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	tc := ix.tail
	if tc != nil && tc.key == key && tc.path == path {
		if tc.offset == size {
			return tc.entries, nil
		}
		if tc.offset < size {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			buf := make([]byte, size-tc.offset)
			n, rerr := f.ReadAt(buf, tc.offset)
			if rerr != nil && rerr != io.EOF {
				return nil, rerr
			}
			buf = buf[:n]
			if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
				fresh := ParseEntries(bytes.NewReader(buf[:i+1]))
				entries := make([]Entry, 0, len(tc.entries)+len(fresh))
				entries = append(entries, tc.entries...)
				entries = append(entries, fresh...)
				ix.tail = &tailCache{key: key, path: path, offset: tc.offset + int64(i+1), entries: entries}
				return ix.tail.entries, nil
			}
			// Nothing complete in the delta yet.
			return tc.entries, nil
		}
	}
	// First view, a different session, or a shrunk file: full parse.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	consumed := int64(len(data))
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 && i != len(data)-1 {
		consumed = int64(i + 1) // a torn trailing line waits for its newline
	}
	entries := ParseEntries(bytes.NewReader(data[:consumed]))
	ix.tail = &tailCache{key: key, path: path, offset: consumed, entries: entries}
	return entries, nil
}

func messageItems(e Entry) []ViewItem {
	m := e.Message
	var out []ViewItem
	switch m.Role {
	case "user":
		it := ViewItem{Kind: "user"}
		for _, b := range m.Blocks() {
			switch b.Type {
			case "text":
				it.Text = strings.TrimSpace(it.Text + "\n" + b.Text)
			case "image":
				it.Images++
			}
		}
		if it.Text != "" || it.Images > 0 {
			out = append(out, it)
		}
	case "assistant":
		for _, b := range m.Blocks() {
			switch b.Type {
			case "thinking":
				if b.Redacted {
					out = append(out, ViewItem{Kind: "thinking", Redacted: true})
					continue
				}
				out = append(out, ViewItem{Kind: "thinking", Text: b.Thinking})
			case "text":
				if b.Text != "" {
					out = append(out, ViewItem{Kind: "text", Text: b.Text})
				}
			case "toolCall":
				out = append(out, ViewItem{Kind: "tool_call", ToolName: b.Name, Text: argsSummary(b.Arguments)})
			}
		}
	case "toolResult":
		it := ViewItem{Kind: "tool_result", ToolName: m.ToolName, IsError: m.IsError, DurationMs: m.DurationMs}
		for _, b := range m.Blocks() {
			if b.Type == "text" {
				it.Text = truncate(b.Text, 400)
				break
			}
		}
		out = append(out, it)
	case "bashExecution":
		// decoded through Content as text; command lives in custom fields
		// per docs; render whatever text is available.
		out = append(out, ViewItem{Kind: "bash", Text: truncate(firstText(m), 400)})
	case "custom":
		out = append(out, ViewItem{Kind: "custom", Text: truncate(firstText(m), 400)})
	}
	return out
}

func firstText(m *Message) string {
	for _, b := range m.Blocks() {
		if b.Type == "text" {
			return b.Text
		}
	}
	return ""
}

func argsSummary(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err == nil {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return strings.Join(keys, ", ")
	}
	return truncate(string(raw), 80)
}

// truncate keeps n runes, so multibyte text never splits mid-rune.
func truncate(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
