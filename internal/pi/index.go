package pi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SessionMeta is the list-page projection of one session file.
type SessionMeta struct {
	Key         string    `json:"key"`
	Path        string    `json:"path"`
	ID          string    `json:"id"`
	Cwd         string    `json:"cwd"`
	Name        string    `json:"name"`
	FirstUser   string    `json:"first_user"`
	Mtime       time.Time `json:"mtime"`
	Version     int       `json:"version"`
	Entries     int       `json:"entries"`
	TotalTokens int64     `json:"total_tokens"`
	TotalCost   float64   `json:"total_cost"`
	// CtxTokens is the newest turn's usage total, a stand-in for the
	// current context-window fill.
	CtxTokens int64  `json:"ctx_tokens,omitempty"`
	Model     string `json:"model"`
	Thinking  string `json:"thinking"`
	// Todos is the latest rpiv-todo-state checklist; empty when the
	// session never ran the todo extension.
	Todos []Todo `json:"todos,omitempty"`
}

// Todo is one row of the todo extension's checklist.
type Todo struct {
	ID         int    `json:"id"`
	Subject    string `json:"subject"`
	Status     string `json:"status"` // pending, in_progress, completed
	ActiveForm string `json:"active_form,omitempty"`
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
	id      fileID
	tail    []byte // up to 64 bytes ending at offset, the revalidation window
}

type metaCache struct {
	mtime time.Time
	size  int64
	id    fileID
	meta  SessionMeta
}

// fileID identifies a file beyond path+mtime+size, the cheap part of
// herdr-web-ui's dev:ino revalidation: a same-path replacement gets a
// fresh inode even when mtime and size match. The zero value means the
// platform has no raw stat and identity falls back to mtime+size.
type fileID struct {
	dev uint64
	ino uint64
}

func statID(st os.FileInfo) fileID {
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		return fileID{dev: sys.Dev, ino: sys.Ino}
	}
	return fileID{}
}

// windowAt reads up to 64 bytes ending at off.
func windowAt(f *os.File, off int64) ([]byte, error) {
	start := off - 64
	if start < 0 {
		start = 0
	}
	buf := make([]byte, off-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, err
	}
	return buf, nil
}

func tailWindow(path string, size int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return windowAt(f, size)
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
	// Key desc on mtime ties: keys are chronological timestamps, so a
	// same-jiffy write batch still shows newest first.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Mtime.Equal(out[j].Mtime) {
			return out[i].Key > out[j].Key
		}
		return out[i].Mtime.After(out[j].Mtime)
	})
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
	if ok && c.id == statID(st) && c.mtime.Equal(st.ModTime()) && c.size == st.Size() {
		return c.meta, true
	}
	meta, ok := ix.parse(path, key, st)
	if !ok {
		return SessionMeta{}, false
	}
	ix.mu.Lock()
	ix.cache[key] = metaCache{mtime: st.ModTime(), size: st.Size(), id: statID(st), meta: meta}
	ix.mu.Unlock()
	return meta, true
}

func (ix *Index) parse(path, key string, st os.FileInfo) (SessionMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return SessionMeta{}, false
	}
	defer f.Close()
	// One pass: header and entries come out of the same read. The old
	// two-pass shape (ParseHeader, then Seek(0) + ParseEntries) read a
	// growing session twice per meta miss.
	h, entries, found := parseAll(f)
	if !found {
		return SessionMeta{}, false
	}

	meta := SessionMeta{
		Key:     key,
		Path:    path,
		ID:      h.ID,
		Cwd:     h.Cwd,
		Mtime:   st.ModTime(),
		Version: h.Version,
		Entries: len(entries),
	}
	todoIdx, lastUserIdx := -1, -1
	for i, e := range entries {
		if u := e.Usage; u != nil {
			meta.TotalTokens += u.TotalTokens
			meta.TotalCost += u.Cost.Total
			meta.CtxTokens = u.TotalTokens
		}
		switch e.Type {
		case "session_info":
			// pi: the latest session_info entry is the session name and an
			// empty name explicitly clears it (getSessionName walks in reverse)
			meta.Name = strings.TrimSpace(e.Name)
		case "model_change":
			if e.ModelID != "" {
				meta.Model = e.Provider + "/" + e.ModelID
			}
		case "thinking_level_change":
			if e.ThinkingLevel != "" {
				meta.Thinking = e.ThinkingLevel
			}
		case "custom":
			if e.CustomType == "rpiv-todo-state" && len(e.Data) > 0 {
				var td struct {
					Tasks []Todo `json:"tasks"`
				}
				// A present-but-empty tasks array is a clear; only an
				// absent key leaves the previous list standing.
				if json.Unmarshal(e.Data, &td) == nil && td.Tasks != nil {
					meta.Todos = td.Tasks
					todoIdx = i
				}
			}
			// pi's auto-namer labels the window with a short name; pi and
			// herdr display it instead of the long session_info name.
			if e.CustomType == "pi-auto-name" && len(e.Data) > 0 {
				var an struct {
					WindowName string `json:"windowName"`
				}
				if json.Unmarshal(e.Data, &an) == nil && an.WindowName != "" {
					meta.Name = an.WindowName
				}
			}
		case "message":
			m := e.Message
			if m == nil {
				continue
			}
			if m.Usage != nil {
				meta.TotalTokens += m.Usage.TotalTokens
				meta.TotalCost += m.Usage.Cost.Total
				meta.CtxTokens = m.Usage.TotalTokens
			}
			if m.Model != "" {
				meta.Model = m.Provider + "/" + m.Model
			}
			if meta.FirstUser == "" && m.Role == "user" {
				meta.FirstUser = firstText(m)
			}
			if m.Role == "user" {
				lastUserIdx = i
			}
		}
	}
	meta.FirstUser = truncate(meta.FirstUser, 80)
	// pi's terminal drops a finished todo panel when the next user
	// message lands; retire the served list the same way so the phone
	// strip does not outlive it. Empty, not nil: a nil list would ride
	// the status frame as null and the client would keep the stale one.
	if todoIdx >= 0 && lastUserIdx > todoIdx && todosAllDone(meta.Todos) {
		meta.Todos = []Todo{}
	}
	return meta, true
}

func todosAllDone(todos []Todo) bool {
	if len(todos) == 0 {
		return false
	}
	for _, t := range todos {
		if t.Status != "completed" {
			return false
		}
	}
	return true
}

// ViewItem is one renderable transcript row.
type ViewItem struct {
	Kind       string `json:"kind"` // user, text, thinking, tool_call, tool_result, marker, bash, custom
	Text       string `json:"text"`
	ToolName   string `json:"tool_name,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
	Redacted   bool   `json:"redacted,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Images     int    `json:"images,omitempty"`
	// Truncated marks items whose text the render path shortened (tool
	// output and bash); the full text stays fetchable via FullItem.
	Truncated bool `json:"truncated,omitempty"`
	// Seq is the item's position on the active branch, stable across
	// tail cuts and cache rereads, so stream clients can key rows by it.
	Seq int `json:"seq"`
}

// Transcript renders the tail of a session's active branch.
func (ix *Index) Transcript(key string, limit int) (SessionMeta, []ViewItem, error) {
	items, err := ix.branchItems(key, renderLimit)
	if err != nil {
		return SessionMeta{}, nil, err
	}
	meta, ok := ix.Meta(key)
	if !ok {
		return meta, nil, fmt.Errorf("session %q not found", key)
	}
	if limit > 0 && len(items) > limit {
		items = items[len(items)-limit:]
	}
	return meta, items, nil
}

// transcriptPageCap bounds one history page: the walk extends past
// tool plumbing until it holds enough conversation items, and this
// stops a conversation-starved stretch from producing an unbounded
// page.
const transcriptPageCap = 120

// TranscriptBefore pages backwards through the active branch: the
// newest page ending at the limit-th conversation item (user or
// assistant text) strictly older than before, ascending. Tool calls,
// results and thinking ride along inside the page so each fetch
// surfaces real messages, with transcriptPageCap bounding the stretch.
// more reports whether older items remain.
func (ix *Index) TranscriptBefore(key string, before int, convLimit int) ([]ViewItem, bool, error) {
	items, err := ix.branchItems(key, renderLimit)
	if err != nil {
		return nil, false, err
	}
	cut := 0
	for cut < len(items) && int(items[cut].Seq) < before {
		cut++
	}
	older := items[:cut]
	taken := len(older)
	if convLimit > 0 {
		taken = 0
		conv := 0
		for i := len(older) - 1; i >= 0; i-- {
			taken = len(older) - i
			if k := older[i].Kind; k == "user" || k == "text" {
				conv++
				if conv >= convLimit {
					break
				}
			}
			if taken >= transcriptPageCap {
				break
			}
		}
	}
	page := older
	more := false
	if taken < len(older) {
		page = older[len(older)-taken:]
		more = true
	}
	return page, more, nil
}

// ItemImage returns the decoded bytes of the item's first image block
// with its mime type. Image parts (user attachments, read-tool results)
// ride the transcript as counts only, so clients fetch bytes here.
func (ix *Index) ItemImage(key string, seq int) ([]byte, string, error) {
	meta, ok := ix.Meta(key)
	if !ok {
		return nil, "", fmt.Errorf("session %q not found", key)
	}
	st, err := os.Stat(meta.Path)
	if err != nil {
		return nil, "", err
	}
	entries, err := ix.tailEntries(key, meta.Path, st)
	if err != nil {
		return nil, "", err
	}
	path := ActivePath(entries)
	pos := 0
	for _, e := range path {
		var vs []ViewItem
		switch {
		case e.Message != nil:
			vs = messageItems(e, 0)
		case e.Type == "compaction", e.Type == "model_change", e.Type == "branch_summary", e.Type == "custom_message":
			vs = []ViewItem{{Kind: "marker"}}
		}
		start := pos
		pos += len(vs)
		if seq < start || seq >= pos {
			continue
		}
		if e.Message != nil {
			for _, b := range e.Message.Blocks() {
				if b.Type != "image" || b.Data == "" {
					continue
				}
				raw, err := base64.StdEncoding.DecodeString(b.Data)
				if err != nil {
					return nil, "", fmt.Errorf("item %d image in %q: %w", seq, key, err)
				}
				mime := b.MimeType
				if mime == "" {
					mime = "application/octet-stream"
				}
				return raw, mime, nil
			}
		}
		return nil, "", fmt.Errorf("item %d has no image in %q", seq, key)
	}
	return nil, "", fmt.Errorf("item %d not found in %q", seq, key)
}

// FullItem returns one transcript item with no render truncation, so
// the client can show whole tool output on demand.
func (ix *Index) FullItem(key string, seq int) (SessionMeta, ViewItem, error) {
	items, err := ix.branchItems(key, 0)
	if err != nil {
		return SessionMeta{}, ViewItem{}, err
	}
	meta, ok := ix.Meta(key)
	if !ok {
		return meta, ViewItem{}, fmt.Errorf("session %q not found", key)
	}
	for _, it := range items {
		if it.Seq == seq {
			return meta, it, nil
		}
	}
	return meta, ViewItem{}, fmt.Errorf("item %d not found in %q", seq, key)
}

// renderLimit is the per-item text cap the transcript render path
// applies; FullItem passes 0 for none.
const renderLimit = 400

// ToolCallDraft returns the full draft message behind the session's
// latest editor-dialog tool call. pi's commit gate previews the tool
// arguments, so the session file carries the complete text that the
// pane can only paint in windows. Empty when no draft-shaped call
// exists.
func (ix *Index) ToolCallDraft(key string) string {
	meta, ok := ix.Meta(key)
	if !ok {
		return ""
	}
	f, err := os.Open(meta.Path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	const window = 256 << 10
	start := st.Size() - window
	if start < 0 {
		start = 0
	}
	raw := make([]byte, st.Size()-start)
	if _, err = f.ReadAt(raw, start); err != nil && err != io.EOF {
		return ""
	}
	// windowAt zero-pads short files, and the window's first line can
	// be a partial entry - both fail to unmarshal and skip cleanly.
	lines := strings.Split(string(bytes.TrimLeft(raw, "\x00")), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		var e Entry
		if json.Unmarshal([]byte(lines[i]), &e) != nil || e.Message == nil {
			continue
		}
		for _, b := range e.Message.Blocks() {
			if b.Type != "toolCall" {
				continue
			}
			var args struct {
				Subject string `json:"subject"`
				Body    string `json:"body"`
				Message string `json:"message"`
			}
			if json.Unmarshal(b.Arguments, &args) != nil {
				continue
			}
			switch {
			case args.Subject != "" && args.Body != "":
				return args.Subject + "\n\n" + args.Body
			case args.Body != "":
				return args.Body
			case args.Message != "":
				return args.Message
			}
		}
	}
	return ""
}

func (ix *Index) branchItems(key string, textLimit int) ([]ViewItem, error) {
	meta, ok := ix.Meta(key)
	if !ok {
		return nil, fmt.Errorf("session %q not found", key)
	}
	st, err := os.Stat(meta.Path)
	if err != nil {
		return nil, err
	}
	entries, err := ix.tailEntries(key, meta.Path, st)
	if err != nil {
		return nil, err
	}
	path := ActivePath(entries)
	// Seq numbers the full branch before any tail cut, so a client sees
	// the same item identity whether it asked for everything or a tail.
	items := make([]ViewItem, 0, len(path))
	seq := 0
	bump := func(vs []ViewItem) {
		for i := range vs {
			vs[i].Seq = seq
			seq++
		}
		items = append(items, vs...)
	}
	for _, e := range path {
		switch {
		case e.Message != nil:
			bump(messageItems(e, textLimit))
		case e.Type == "compaction":
			bump([]ViewItem{{Kind: "marker", Text: fmt.Sprintf("Context compacted (%d tokens before)", e.TokensBefore)}})
		case e.Type == "model_change":
			bump([]ViewItem{{Kind: "marker", Text: "Model -> " + e.Provider + "/" + e.ModelID}})
		case e.Type == "branch_summary":
			bump([]ViewItem{{Kind: "marker", Text: "Branch summary: " + truncate(e.Summary, 140)}})
		case e.Type == "custom_message":
			bump([]ViewItem{{Kind: "custom", Text: "custom: " + truncate(e.CustomType, 40)}})
		}
	}
	return items, nil
}

// tailEntries returns the session's entries, parsing only the bytes
// appended since the previous call for this session. The offset always
// sits on a newline boundary, so a line the writer has not finished is
// picked up once it is complete, never duplicated.
//
// Revalidation is content-based, the cheap part of herdr-web-ui's
// approach: the file's inode must match the cached one, and the 64
// bytes ending at the cached offset must be unchanged. That catches
// in-place same-size rewrites and same-path replacements that fool
// mtime+size, at the cost of one 64-byte pread per call.
//
// The cache check and the commit run under the lock, but the file read
// and parse run OUTSIDE it: a slow full parse must never head-of-line
// block the cachedMeta lookups that share the mutex. Concurrent callers
// may parse the same delta twice; a commit only lands when the cache
// still holds the state it was computed from, and after two lost races
// the last write wins - the entries are level-triggered from file
// bytes, so racing results are identical.
func (ix *Index) tailEntries(key, path string, st os.FileInfo) ([]Entry, error) {
	size := st.Size()
	for attempt := 0; attempt < 3; attempt++ {
		ix.mu.Lock()
		tc := ix.tail
		same := tc != nil && tc.key == key && tc.path == path && tc.id == statID(st)
		if same && tc.offset == size {
			out := tc.entries
			ix.mu.Unlock()
			if win, err := tailWindow(path, size); err != nil || !bytes.Equal(win, tc.tail) {
				break // the bytes above the end changed in place: full reparse
			}
			return out, nil
		}
		if same && tc.offset < size {
			base := tc
			ix.mu.Unlock()
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			if win, werr := windowAt(f, base.offset); werr != nil {
				f.Close()
				break // unreadable mid-check: the full parse below decides
			} else if !bytes.Equal(win, base.tail) {
				f.Close()
				break // an in-place edit above the delta invalidates the tail
			}
			buf := make([]byte, size-base.offset)
			n, rerr := f.ReadAt(buf, base.offset)
			if rerr != nil && rerr != io.EOF {
				f.Close()
				return nil, rerr
			}
			buf = buf[:n]
			if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
				nextOffset := base.offset + int64(i+1)
				nwin, nerr := windowAt(f, nextOffset)
				f.Close()
				if nerr != nil {
					break // shrank under us: the full parse decides
				}
				fresh := ParseEntries(bytes.NewReader(buf[:i+1]))
				entries := make([]Entry, 0, len(base.entries)+len(fresh))
				entries = append(entries, base.entries...)
				entries = append(entries, fresh...)
				next := &tailCache{key: key, path: path, offset: nextOffset, entries: entries, id: statID(st), tail: nwin}
				ix.mu.Lock()
				if ix.tail == base { // nobody advanced the cache meanwhile
					ix.tail = next
					ix.mu.Unlock()
					return next.entries, nil
				}
				ix.mu.Unlock()
				continue // the cache moved under us: recompute from the newer state
			}
			f.Close()
			// Nothing complete in the delta yet.
			return base.entries, nil
		}
		ix.mu.Unlock()
		break
		// First view, a different session, a shrunk or replaced file, an
		// in-place rewrite, or a lost race on the delta path: full parse.
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return nil, err
	}
	consumed := int64(len(data))
	wstart := consumed - 64
	if wstart < 0 {
		wstart = 0
	}
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 && i != len(data)-1 {
		consumed = int64(i + 1) // a torn trailing line waits for its newline
		wstart = consumed - 64
		if wstart < 0 {
			wstart = 0
		}
	}
	win := append([]byte(nil), data[wstart:consumed]...)
	entries := ParseEntries(bytes.NewReader(data[:consumed]))
	ix.mu.Lock()
	ix.tail = &tailCache{key: key, path: path, offset: consumed, entries: entries, id: statID(st), tail: win}
	ix.mu.Unlock()
	return entries, nil
}

func messageItems(e Entry, textLimit int) []ViewItem {
	m := e.Message
	var out []ViewItem
	limit := func(s string) (string, bool) {
		if textLimit <= 0 {
			return s, false
		}
		// Compare source length: truncate() also flattens newlines, so a
		// changed string does not mean the render actually cut text.
		return truncate(s, textLimit), len([]rune(s)) > textLimit
	}
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
			if b.Type == "text" && it.Text == "" {
				it.Text, it.Truncated = limit(b.Text)
			}
			if b.Type == "image" {
				it.Images++
			}
		}
		out = append(out, it)
	case "bashExecution":
		// decoded through Content as text; command lives in custom fields
		// per docs; render whatever text is available.
		it := ViewItem{Kind: "bash"}
		it.Text, it.Truncated = limit(firstText(m))
		out = append(out, it)
	case "custom":
		it := ViewItem{Kind: "custom"}
		it.Text, it.Truncated = limit(firstText(m))
		out = append(out, it)
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

// argsSummary surfaces the one argument that names what the tool did
// (a path, a command), so the chat can title tool rows the way code
// clients do. JSON key order is not stable, so the pick runs over a
// fixed priority list before falling back to sorted key: value pairs.
var argPriority = []string{
	"path", "file_path", "filepath", "file", "cwd", "command", "cmd",
	"url", "query", "pattern", "topic", "name", "subject",
}

func argsSummary(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return truncate(string(raw), 80)
	}
	for _, k := range argPriority {
		if v, ok := m[k].(string); ok && v != "" {
			return truncate(v, 80)
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				parts = append(parts, k+": "+v)
			}
		case float64:
			parts = append(parts, fmt.Sprintf("%s: %v", k, v))
		case bool:
			parts = append(parts, fmt.Sprintf("%s: %v", k, v))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return truncate(strings.Join(parts, ", "), 120)
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
