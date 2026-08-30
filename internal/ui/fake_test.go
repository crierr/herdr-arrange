package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crierr/herdr-arrange/internal/engine"
	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

// The pane the fixture arranges.
const (
	curPane = "w1S:p2"
	curTab  = "w1S:t1"
	curWS   = "w1S"
)

// fakeClient is a stand-in for the herdr socket, recording what the UI asked for.
//
// It models herdr only as far as the UI can tell the difference: it keeps its
// layout coherent across moves so the status line stays meaningful, and leaves the
// real semantics — same-tab refusals, tab auto-close, permutation planning — to
// the engine's own fake, which models them properly.
type fakeClient struct {
	layout   *herdr.LayoutDescription
	snapshot *herdr.SessionSnapshot
	calls    []string

	// swapRefused makes pane.swap report changed:false, the way herdr does when
	// there is no neighbour in that direction. resizeRefused is the same for
	// pane.resize, which herdr refuses when the ratio is already at its clamp.
	swapRefused   bool
	resizeRefused bool
	// exportErr is returned by every layout.export, for testing a vanished pane.
	exportErr error
	// geoErr fails pane.layout alone, which costs the minimap and nothing else.
	geoErr error
	// opErr fails the next mutating call.
	opErr error

	// arranged is the pane the UI operates on; only its moves rewrite the
	// fixture layout.
	arranged string
	nextTab  int
}

func newFakeClient(root *tree.Node) *fakeClient {
	f := &fakeClient{
		layout: &herdr.LayoutDescription{
			WorkspaceID: curWS, TabID: curTab, FocusedPaneID: curPane, Root: *toLayout(root),
		},
		arranged: curPane,
		nextTab:  9,
	}
	f.snapshot = fixtureSnapshot(root.Leaves())
	return f
}

func (f *fakeClient) record(format string, args ...any) {
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

// took reports whether a call matching substr was made, and forgets the log so
// each assertion in a table test starts clean.
func (f *fakeClient) took(substr string) bool {
	for _, c := range f.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func (f *fakeClient) log() string { return strings.Join(f.calls, "; ") }

func (f *fakeClient) fail() error {
	err := f.opErr
	f.opErr = nil
	return err
}

func (f *fakeClient) ExportLayoutForPane(_ context.Context, paneID string) (*herdr.LayoutDescription, error) {
	f.record("export %s", paneID)
	if f.exportErr != nil {
		return nil, f.exportErr
	}
	return f.layout, nil
}

func (f *fakeClient) Snapshot(_ context.Context) (*herdr.SessionSnapshot, error) {
	f.record("snapshot")
	return f.snapshot, nil
}

// fakeArea is the screen the fixture tab is drawn in, and the one the minimap is
// therefore a picture of: four columns of 84 rows fit it exactly.
var fakeArea = herdr.LayoutRect{X: 8, Y: 1, Width: 336, Height: 84}

func (f *fakeClient) PaneLayout(_ context.Context, paneID string) (*herdr.LayoutSnapshot, error) {
	f.record("layout %s", paneID)
	if f.exportErr != nil {
		return nil, f.exportErr
	}
	if f.geoErr != nil {
		return nil, f.geoErr
	}
	snap := &herdr.LayoutSnapshot{
		WorkspaceID: f.layout.WorkspaceID, TabID: f.layout.TabID,
		Area: fakeArea, FocusedPaneID: f.layout.FocusedPaneID,
	}
	snap.Panes = paneRects(tree.FromLayout(&f.layout.Root), fakeArea, f.arranged, nil)
	return snap, nil
}

// paneRects lays a tree out in an area the way herdr's own split_rect does: the
// first child is rounded to size and the second gets what is left, so the panes tile
// the area exactly however the ratios fall.
func paneRects(n *tree.Node, area herdr.LayoutRect, focused string, out []herdr.LayoutPane) []herdr.LayoutPane {
	if n == nil {
		return out
	}
	if n.IsLeaf() {
		return append(out, herdr.LayoutPane{PaneID: n.PaneID, Focused: n.PaneID == focused, Rect: area})
	}
	first, second := area, area
	if n.Dir == herdr.Right {
		first.Width = int(float64(area.Width)*n.Ratio + 0.5)
		second.X, second.Width = area.X+first.Width, area.Width-first.Width
	} else {
		first.Height = int(float64(area.Height)*n.Ratio + 0.5)
		second.Y, second.Height = area.Y+first.Height, area.Height-first.Height
	}
	return paneRects(n.Second, second, focused, paneRects(n.First, first, focused, out))
}

func (f *fakeClient) ResizePane(_ context.Context, paneID string, dir herdr.Direction, amount float64) (*herdr.ResizeResult, error) {
	f.record("resize %s %s %.2f", paneID, dir, amount)
	if err := f.fail(); err != nil {
		return nil, err
	}
	if f.resizeRefused {
		return &herdr.ResizeResult{PaneID: paneID, Reason: "unchanged"}, nil
	}
	return &herdr.ResizeResult{Changed: true, PaneID: paneID, FocusedPaneID: paneID}, nil
}

func (f *fakeClient) SwapDirection(_ context.Context, paneID string, dir herdr.Direction) (*herdr.SwapResult, error) {
	f.record("swap-dir %s %s", paneID, dir)
	if err := f.fail(); err != nil {
		return nil, err
	}
	if f.swapRefused {
		return &herdr.SwapResult{SourcePaneID: paneID, Reason: "no_neighbor"}, nil
	}
	return &herdr.SwapResult{Changed: true, SourcePaneID: paneID, FocusedPaneID: paneID}, nil
}

func (f *fakeClient) SwapPanes(_ context.Context, source, target string) (*herdr.SwapResult, error) {
	f.record("swap %s %s", source, target)
	if err := f.fail(); err != nil {
		return nil, err
	}
	return &herdr.SwapResult{Changed: true, SourcePaneID: source, TargetPaneID: target}, nil
}

func (f *fakeClient) MovePane(_ context.Context, paneID string, dest herdr.Destination, _ bool) (*herdr.MoveResult, error) {
	f.record("move %s -> %s %s %s", paneID, dest.Type, dest.TabID+dest.WorkspaceID, dest.TargetPaneID)
	if err := f.fail(); err != nil {
		return nil, err
	}

	res := &herdr.MoveResult{Changed: true, PreviousPaneID: paneID, PreviousTabID: f.layout.TabID}

	// A rebuild shuffles the tab's other panes through a scratch tab. The UI never
	// looks at those, so the fixture layout is left alone for them and only the
	// arranged pane's own moves retarget it.
	if paneID != f.arranged {
		res.Pane = herdr.PaneInfo{PaneID: paneID, TabID: "w1S:t99", WorkspaceID: curWS}
		res.FocusedPaneID = f.arranged
		return res, nil
	}

	switch dest.Type {
	case "new_workspace":
		// Pane ids are workspace-scoped, so crossing a workspace renames the pane.
		f.arranged = "w2A:p1"
		f.layout = &herdr.LayoutDescription{
			WorkspaceID: "w2A", TabID: "w2A:t1", Root: *toLayout(tree.Leaf(f.arranged)),
		}
	case "new_tab":
		ws := dest.WorkspaceID
		if ws == "" {
			ws = f.layout.WorkspaceID
		}
		f.nextTab++
		f.layout = &herdr.LayoutDescription{
			WorkspaceID: ws,
			TabID:       fmt.Sprintf("%s:t%d", ws, f.nextTab),
			Root:        *toLayout(tree.Leaf(paneID)),
		}
	case "tab":
		host := dest.TargetPaneID
		if host == "" {
			host = "w1S:p7"
		}
		f.layout = &herdr.LayoutDescription{
			WorkspaceID: f.layout.WorkspaceID, TabID: dest.TabID,
			Root: *toLayout(tree.Split(herdr.Down, 0.5, tree.Leaf(host), tree.Leaf(paneID))),
		}
	}

	res.Pane = herdr.PaneInfo{
		PaneID:      f.arranged,
		TabID:       f.layout.TabID,
		WorkspaceID: f.layout.WorkspaceID,
	}
	f.layout.FocusedPaneID = f.arranged
	return res, nil
}

func (f *fakeClient) SetSplitRatio(_ context.Context, tabID string, path []bool, ratio float64) error {
	f.record("ratio %s %v = %.2f", tabID, path, ratio)
	return f.fail()
}

func (f *fakeClient) FocusPane(_ context.Context, paneID string) error {
	f.record("focus %s", paneID)
	return nil
}

func (f *fakeClient) Zoom(_ context.Context, paneID, mode string) (*herdr.ZoomResult, error) {
	f.record("zoom %s %s", paneID, mode)
	return &herdr.ZoomResult{PaneID: paneID}, nil
}

// fixtureSnapshot is the session the tree view is tested against: two workspaces,
// three tabs, and the arranged pane in the middle of the first one.
//
//	[1] w1S  herdr-arrange
//	    ├─ t1  main
//	    │  ├─ p1 … p4        (p2 is the arranged pane)
//	    ├─ t2  logs
//	    │  └─ p7
//	    └─ [c] new tab in this workspace
//	[2] wJ   notes
//	    └─ t1
//	       └─ p1
//	[N] new workspace
func fixtureSnapshot(panes []string) *herdr.SessionSnapshot {
	s := &herdr.SessionSnapshot{
		Version: "0.8.2", Protocol: 21,
		FocusedWorkspaceID: curWS, FocusedTabID: curTab, FocusedPaneID: curPane,
		Workspaces: []herdr.WorkspaceInfo{
			{WorkspaceID: curWS, Number: 1, Label: "herdr-arrange", Focused: true, ActiveTabID: curTab},
			{WorkspaceID: "wJ", Number: 2, Label: "notes", ActiveTabID: "wJ:t1"},
		},
		Tabs: []herdr.TabInfo{
			{TabID: curTab, WorkspaceID: curWS, Number: 1, Label: "main", Focused: true, PaneCount: len(panes)},
			{TabID: "w1S:t2", WorkspaceID: curWS, Number: 2, Label: "logs", PaneCount: 1},
			{TabID: "wJ:t1", WorkspaceID: "wJ", Number: 1, PaneCount: 1},
		},
	}
	for _, pane := range panes {
		info := herdr.PaneInfo{PaneID: pane, TabID: curTab, WorkspaceID: curWS, TerminalID: "term-" + pane}
		if pane == curPane {
			info.Focused, info.Agent = true, "claude"
		}
		s.Panes = append(s.Panes, info)
	}
	s.Panes = append(s.Panes,
		herdr.PaneInfo{PaneID: "w1S:p7", TabID: "w1S:t2", WorkspaceID: curWS, Label: "tail"},
		herdr.PaneInfo{PaneID: "wJ:p1", TabID: "wJ:t1", WorkspaceID: "wJ"},
	)
	// herdr reports the area it draws each tab in, which is also the room a popup
	// has: an ordinary terminal, so the tree is sized by its own height here.
	for _, tab := range s.Tabs {
		s.Layouts = append(s.Layouts, herdr.LayoutSnapshot{
			TabID: tab.TabID, Area: herdr.LayoutRect{Width: 200, Height: 50},
		})
	}
	return s
}

// evenFour is the starting layout: [[p1 | p2] | [p3 | p4]], which is what
// even-horizontal builds for four panes.
func evenFour() *tree.Node {
	return tree.Split(herdr.Right, 0.5,
		tree.Split(herdr.Right, 0.5, tree.Leaf("w1S:p1"), tree.Leaf(curPane)),
		tree.Split(herdr.Right, 0.5, tree.Leaf("w1S:p3"), tree.Leaf("w1S:p4")),
	)
}

func toLayout(n *tree.Node) *herdr.LayoutNode {
	if n.IsLeaf() {
		return &herdr.LayoutNode{Type: "pane", PaneID: n.PaneID}
	}
	return &herdr.LayoutNode{
		Type: "split", Direction: n.Dir, Ratio: n.Ratio,
		First: toLayout(n.First), Second: toLayout(n.Second),
	}
}

// driving the model

// start builds a model, runs its initial load and gives it a popup-sized window.
// It says nothing about the size the popup was asked for, which is how a hand-run UI
// starts: switching views then never reopens anything.
func start(t *testing.T, f *fakeClient, mode Mode) Model {
	t.Helper()
	return startWith(t, f, Options{Mode: mode})
}

// startWith is start for the tests that care what size the action asked herdr for,
// which is what decides whether a mode switch reopens the popup.
func startWith(t *testing.T, f *fakeClient, opts Options) Model {
	t.Helper()
	m := New(engine.New(f, "", curPane, curTab, curWS), opts)
	m = settle(t, m, m.Init())
	m = send(t, m, tea.WindowSizeMsg{Width: 66, Height: 15})
	f.calls = nil
	return m
}

// sizeMsg is the resize herdr sends when the popup is created or the terminal
// changes size.
func sizeMsg(width, height int) tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: width, Height: height}
}

// send delivers messages and runs the resulting commands to completion, so the
// test observes a settled model rather than a pending one.
func send(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, cmd := m.Update(msg)
		m = settle(t, next.(Model), cmd)
	}
	return m
}

// settle runs a command chain until the model stops producing work.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 20 {
			t.Fatal("the model never stopped issuing commands")
		}
		msg := cmd()
		if msg == nil {
			break
		}
		next, c := m.Update(msg)
		m, cmd = next.(Model), c
	}
	return m
}

// press sends a keypress by name.
func press(t *testing.T, m Model, names ...string) Model {
	t.Helper()
	for _, name := range names {
		m = send(t, m, key(t, name))
	}
	return m
}

// key builds the KeyMsg for a key name and checks it against bubbletea's own
// String(), so a test can never assert on a key the UI would never see.
func key(t *testing.T, name string) tea.KeyMsg {
	t.Helper()

	special := map[string]tea.KeyType{
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"shift+up": tea.KeyShiftUp, "shift+down": tea.KeyShiftDown,
		"shift+left": tea.KeyShiftLeft, "shift+right": tea.KeyShiftRight,
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, " ": tea.KeySpace,
		"home": tea.KeyHome, "end": tea.KeyEnd,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"ctrl+c": tea.KeyCtrlC, "ctrl+d": tea.KeyCtrlD, "ctrl+u": tea.KeyCtrlU,
		"ctrl+h": tea.KeyCtrlH, "ctrl+j": tea.KeyCtrlJ,
		"ctrl+k": tea.KeyCtrlK, "ctrl+l": tea.KeyCtrlL,
		"ctrl+up": tea.KeyCtrlUp, "ctrl+down": tea.KeyCtrlDown,
		"ctrl+left": tea.KeyCtrlLeft, "ctrl+right": tea.KeyCtrlRight,
	}

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
	if typ, ok := special[name]; ok {
		msg = tea.KeyMsg{Type: typ}
	}
	if msg.String() != name {
		t.Fatalf("key %q builds as %q", name, msg.String())
	}
	return msg
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// plain strips styling, so assertions do not depend on the colour profile the
// test happens to run under.
func plain(s string) string { return ansi.ReplaceAllString(s, "") }

// lines returns the model's rendered view, unstyled.
func lines(m Model) []string { return strings.Split(plain(m.View()), "\n") }
