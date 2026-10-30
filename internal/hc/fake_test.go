package hc

import (
	"bufio"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
)

// fakeCore is a minimal hc core: a unix socket answering one JSON-RPC
// request per connection with a canned handler, recording every request.
type fakeCore struct {
	t  *testing.T
	ln net.Listener

	mu       sync.Mutex
	requests []request
	// handler answers method calls; nil means {} with no error.
	handler func(method string, params json.RawMessage) (any, *Error)
	// afterReply receives the raw connection after the reply is
	// written; returning true holds the connection open (events
	// subscriptions) until the client or test closes it.
	afterReply func(conn net.Conn, method string) bool
}

type request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func startFakeCore(t *testing.T, handler func(string, json.RawMessage) (any, *Error)) *fakeCore {
	t.Helper()
	ln, err := net.Listen("unix", t.TempDir()+"/core.sock")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeCore{t: t, ln: ln, handler: handler}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeCore) socket() string { return f.ln.Addr().String() }

func (f *fakeCore) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		go f.handle(conn)
	}
}

func (f *fakeCore) handle(conn net.Conn) {
	defer conn.Close()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil && !strings.HasSuffix(line, "\n") {
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal([]byte(line), &req) != nil {
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, request{Method: req.Method, Params: req.Params})
	handler := f.handler
	f.mu.Unlock()

	var result any = map[string]any{}
	var rpcErr *Error
	if handler != nil {
		result, rpcErr = handler(req.Method, req.Params)
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if rpcErr != nil {
		resp["error"] = rpcErr
	} else {
		resp["result"] = result
	}
	out, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(out, '\n'))
	f.mu.Lock()
	after := f.afterReply
	f.mu.Unlock()
	if after != nil && after(conn, req.Method) {
		// Hold the connection: reads drain client closes.
		_, _ = bufio.NewReader(conn).ReadString('\n')
	}
}

// seen returns the requests served for method, parsed.
func (f *fakeCore) seen(method string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, r := range f.requests {
		if r.Method != method {
			continue
		}
		var params map[string]any
		if json.Unmarshal(r.Params, &params) == nil {
			out = append(out, params)
		}
	}
	return out
}
