package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakeServer mimics herdr: newline-delimited JSON, one request per connection,
// then close. Anything the client gets wrong about that shape shows up here.
type fakeServer struct {
	t    *testing.T
	ln   net.Listener
	path string

	mu       sync.Mutex
	requests []map[string]any
	handler  func(method string, params map[string]any) (any, *APIError)
}

func newFakeServer(t *testing.T, handler func(string, map[string]any) (any, *APIError)) *fakeServer {
	t.Helper()
	// Not t.TempDir(): it embeds the test name, and a Unix socket path is
	// capped near 104 bytes on macOS.
	dir, err := os.MkdirTemp("", "hs")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeServer{t: t, ln: ln, path: path, handler: handler}
	go s.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return s
}

func (s *fakeServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeServer) handle(conn net.Conn) {
	// One request per connection, then close — exactly like herdr.
	defer conn.Close()

	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return
	}
	var req struct {
		ID     string         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return
	}

	s.mu.Lock()
	s.requests = append(s.requests, map[string]any{"id": req.ID, "method": req.Method, "params": req.Params})
	s.mu.Unlock()

	result, apiErr := s.handler(req.Method, req.Params)
	var out any
	if apiErr != nil {
		out = map[string]any{"id": req.ID, "error": apiErr}
	} else {
		out = map[string]any{"id": req.ID, "result": result}
	}
	body, err := json.Marshal(out)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(body, '\n'))
}

func (s *fakeServer) client() *Client { return &Client{SocketPath: s.path} }

func (s *fakeServer) seen() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func TestCallSendsEnvelopeAndDecodesResult(t *testing.T) {
	s := newFakeServer(t, func(method string, _ map[string]any) (any, *APIError) {
		if method != "layout.export" {
			t.Errorf("method = %q", method)
		}
		return map[string]any{
			"type": "layout_export",
			"layout": map[string]any{
				"workspace_id":    "w1S",
				"tab_id":          "w1S:t1",
				"zoomed":          false,
				"focused_pane_id": "w1S:p2",
				"root": map[string]any{
					"type":      "split",
					"direction": "right",
					"ratio":     0.5,
					"first":     map[string]any{"type": "pane", "pane_id": "w1S:p1"},
					"second":    map[string]any{"type": "pane", "pane_id": "w1S:p2"},
				},
			},
		}, nil
	})

	layout, err := s.client().ExportLayout(context.Background(), "w1S:t1")
	if err != nil {
		t.Fatalf("ExportLayout: %v", err)
	}
	if layout.TabID != "w1S:t1" || layout.FocusedPaneID != "w1S:p2" {
		t.Fatalf("layout = %+v", layout)
	}
	if layout.Root.Type != "split" || layout.Root.Direction != Right || layout.Root.Ratio != 0.5 {
		t.Fatalf("root = %+v", layout.Root)
	}
	if !layout.Root.First.IsPane() || layout.Root.First.PaneID != "w1S:p1" {
		t.Fatalf("first = %+v", layout.Root.First)
	}

	seen := s.seen()
	if len(seen) != 1 {
		t.Fatalf("requests = %d", len(seen))
	}
	if seen[0]["id"] == "" {
		t.Error("request carried no id")
	}
	if got := seen[0]["params"].(map[string]any)["tab_id"]; got != "w1S:t1" {
		t.Errorf("tab_id = %v", got)
	}
}

func TestCallSendsEmptyParamsObject(t *testing.T) {
	// `params` is required by the server even for parameterless methods.
	s := newFakeServer(t, func(_ string, _ map[string]any) (any, *APIError) {
		return map[string]any{"type": "session_snapshot", "snapshot": map[string]any{
			"version": "0.8.2", "protocol": 21,
			"workspaces": []any{}, "tabs": []any{}, "panes": []any{}, "layouts": []any{}, "agents": []any{},
		}}, nil
	})
	if _, err := s.client().Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	raw, err := json.Marshal(s.seen()[0]["params"])
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{}" {
		t.Errorf("params = %s, want {}", raw)
	}
}

// TestSnapshotDecodesTheTabArea: the area a tab is drawn in is the only thing the
// API says about the size of the screen, and tree mode's popup is sized from it.
func TestSnapshotDecodesTheTabArea(t *testing.T) {
	s := newFakeServer(t, func(_ string, _ map[string]any) (any, *APIError) {
		return map[string]any{"type": "session_snapshot", "snapshot": map[string]any{
			"version": "0.8.2", "protocol": 20,
			"workspaces": []any{}, "tabs": []any{}, "panes": []any{},
			"layouts": []any{map[string]any{
				"tab_id": "w1S:t1", "zoomed": false,
				"area":  map[string]any{"x": 26, "y": 0, "width": 336, "height": 84},
				"panes": []any{},
			}},
		}}, nil
	})

	snapshot, err := s.client().Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snapshot.Layouts) != 1 {
		t.Fatalf("layouts = %+v", snapshot.Layouts)
	}
	if got := snapshot.Layouts[0].Area; got.Width != 336 || got.Height != 84 {
		t.Errorf("area = %+v, want 336x84", got)
	}
}

func TestCallSurfacesAPIError(t *testing.T) {
	s := newFakeServer(t, func(_ string, _ map[string]any) (any, *APIError) {
		return nil, &APIError{Code: "ui_busy", Message: "a dialog is open"}
	})

	err := s.client().OpenPopup(context.Background(), PopupOptions{PluginID: "soh.arrange", Entrypoint: "ui"})
	if err == nil {
		t.Fatal("want error")
	}
	if Code(err) != "ui_busy" {
		t.Errorf("Code = %q, want ui_busy", Code(err))
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Message != "a dialog is open" {
		t.Errorf("err = %v", err)
	}
}

func TestEachCallDialsAFreshConnection(t *testing.T) {
	// The real server closes after one response, so reuse is not an option.
	// Two sequential calls must both succeed.
	s := newFakeServer(t, func(_ string, _ map[string]any) (any, *APIError) {
		return map[string]any{"type": "pong", "version": "0.8.2", "protocol": 21}, nil
	})
	c := s.client()
	for i := range 3 {
		if err := c.Call(context.Background(), "ping", nil, nil); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	seen := s.seen()
	if len(seen) != 3 {
		t.Fatalf("server saw %d requests, want 3", len(seen))
	}
	ids := map[string]bool{}
	for _, r := range seen {
		id := r["id"].(string)
		if ids[id] {
			t.Errorf("duplicate request id %q", id)
		}
		ids[id] = true
	}
}

func TestMovePaneEncodesDestination(t *testing.T) {
	ratio := 0.3
	tests := []struct {
		name string
		dest Destination
		want map[string]any
	}{
		{
			name: "tab",
			dest: DestTab("w1S:t1", "w1S:p2", Down, &ratio),
			want: map[string]any{"type": "tab", "tab_id": "w1S:t1", "target_pane_id": "w1S:p2", "split": "down", "ratio": 0.3},
		},
		{
			name: "tab without ratio or target",
			dest: DestTab("w1S:t1", "", Right, nil),
			want: map[string]any{"type": "tab", "tab_id": "w1S:t1", "split": "right"},
		},
		{
			name: "new tab",
			dest: DestNewTab("w1S", "arrange:parking"),
			want: map[string]any{"type": "new_tab", "workspace_id": "w1S", "label": "arrange:parking"},
		},
		{
			name: "new workspace",
			dest: DestNewWorkspace("", ""),
			want: map[string]any{"type": "new_workspace"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newFakeServer(t, func(_ string, _ map[string]any) (any, *APIError) {
				return map[string]any{"type": "pane_move", "move_result": map[string]any{
					"changed":               true,
					"pane":                  map[string]any{"pane_id": "w1S:p2", "terminal_id": "t", "workspace_id": "w1S", "tab_id": "w1S:t1", "focused": true, "revision": 1},
					"previous_pane_id":      "w1S:p2",
					"previous_tab_id":       "w1S:t2",
					"previous_workspace_id": "w1S",
					"focused_pane_id":       "w1S:p2",
					"target_layout":         map[string]any{},
				}}, nil
			})

			res, err := s.client().MovePane(context.Background(), "w1S:p2", tc.dest, true)
			if err != nil {
				t.Fatalf("MovePane: %v", err)
			}
			if !res.Changed || res.Pane.PaneID != "w1S:p2" {
				t.Fatalf("result = %+v", res)
			}

			got := s.seen()[0]["params"].(map[string]any)
			if got["focus"] != true {
				t.Errorf("focus = %v", got["focus"])
			}
			gotDest := got["destination"].(map[string]any)
			wantJSON, _ := json.Marshal(tc.want)
			gotJSON, _ := json.Marshal(gotDest)
			if !jsonEqual(gotJSON, wantJSON) {
				t.Errorf("destination = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestSetSplitRatioEncodesEmptyRootPath(t *testing.T) {
	// The root split is addressed by an empty path, which must serialise as []
	// rather than null.
	s := newFakeServer(t, func(_ string, _ map[string]any) (any, *APIError) {
		return map[string]any{"type": "layout_split_ratio_set", "layout": map[string]any{
			"workspace_id": "w1S", "tab_id": "w1S:t1", "zoomed": false,
			"focused_pane_id": "w1S:p1", "root": map[string]any{"type": "pane", "pane_id": "w1S:p1"},
		}}, nil
	})
	if err := s.client().SetSplitRatio(context.Background(), "w1S:t1", nil, 0.5); err != nil {
		t.Fatalf("SetSplitRatio: %v", err)
	}
	raw, _ := json.Marshal(s.seen()[0]["params"].(map[string]any)["path"])
	if string(raw) != "[]" {
		t.Errorf("path = %s, want []", raw)
	}
}

func TestPopupSizeMarshalling(t *testing.T) {
	for _, tc := range []struct {
		size PopupSize
		want string
	}{
		{Cells(66), "66"},
		{Percent(80), `"80%"`},
	} {
		got, err := json.Marshal(tc.size)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Errorf("Marshal(%+v) = %s, want %s", tc.size, got, tc.want)
		}
	}
}

func TestPaneInfoDisplayName(t *testing.T) {
	for _, tc := range []struct {
		pane PaneInfo
		want string
	}{
		{PaneInfo{PaneID: "w1S:p1", Label: "editor", Agent: "claude", Title: "nvim"}, "editor"},
		{PaneInfo{PaneID: "w1S:p1", Agent: "claude", Title: "nvim"}, "claude"},
		{PaneInfo{PaneID: "w1S:p1", Title: "nvim"}, "nvim"},
		{PaneInfo{PaneID: "w1S:p1"}, "w1S:p1"},
	} {
		if got := tc.pane.DisplayName(); got != tc.want {
			t.Errorf("DisplayName(%+v) = %q, want %q", tc.pane, got, tc.want)
		}
	}
}

func TestNewRequiresSocketPath(t *testing.T) {
	t.Setenv("HERDR_SOCKET_PATH", "")
	if _, err := New(); err == nil {
		t.Error("want error when HERDR_SOCKET_PATH is unset")
	}
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/x.sock")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if c.SocketPath != "/tmp/x.sock" {
		t.Errorf("SocketPath = %q", c.SocketPath)
	}
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &y); err != nil {
		return false
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}
