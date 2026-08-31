package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

// fakeHerdr models the parts of herdr's behaviour the executor depends on: how
// pane.move restructures trees, that it refuses a same-tab destination, that it
// refuses a zoomed tab, that a tab closes when its last pane leaves, and that
// crossing a workspace boundary renames the pane.
//
// Getting any of these wrong in the real server would corrupt a layout, so the
// executor is tested against them rather than against recorded calls.
type fakeHerdr struct {
	tabs      map[string]*tabState
	tabOrder  []string
	focused   string
	nextTab   int
	nextWS    int
	nextPane  int
	callCount int
	calls     []string

	// failAfter makes the call with this 1-based index fail, for testing what an
	// interrupted rebuild leaves behind. Zero means never fail.
	failAfter int
}

type tabState struct {
	id          string
	workspaceID string
	label       string
	root        *tree.Node
	zoomed      bool
}

func newFakeHerdr() *fakeHerdr {
	return &fakeHerdr{tabs: map[string]*tabState{}, nextTab: 1, nextWS: 2, nextPane: 100}
}

// withTab seeds a tab from a tree, and returns the fake for chaining.
func (f *fakeHerdr) withTab(workspaceID string, root *tree.Node) *fakeHerdr {
	id := fmt.Sprintf("%s:t%d", workspaceID, f.nextTab)
	f.nextTab++
	f.tabs[id] = &tabState{id: id, workspaceID: workspaceID, root: root}
	f.tabOrder = append(f.tabOrder, id)
	if f.focused == "" {
		f.focused = root.FirstLeaf()
	}
	return f
}

// tabOf finds the tab holding a pane.
func (f *fakeHerdr) tabOf(paneID string) *tabState {
	for _, id := range f.tabOrder {
		if t, ok := f.tabs[id]; ok && t.root.Has(paneID) {
			return t
		}
	}
	return nil
}

func (f *fakeHerdr) treeOf(tabID string) *tree.Node {
	if t, ok := f.tabs[tabID]; ok {
		return t.root
	}
	return nil
}

// liveTabs returns the ids of tabs that still exist, in creation order.
func (f *fakeHerdr) liveTabs() []string {
	var out []string
	for _, id := range f.tabOrder {
		if _, ok := f.tabs[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// allPanes returns every pane in the session.
func (f *fakeHerdr) allPanes() []string {
	var out []string
	for _, id := range f.liveTabs() {
		out = append(out, f.tabs[id].root.Leaves()...)
	}
	return out
}

// track records a call and applies fault injection.
func (f *fakeHerdr) track(format string, args ...any) error {
	f.callCount++
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	if f.failAfter != 0 && f.callCount == f.failAfter {
		return fmt.Errorf("injected failure on call %d", f.callCount)
	}
	return nil
}

func (f *fakeHerdr) callLog() string { return strings.Join(f.calls, "\n") }

// dropTab removes a tab that has no panes left, the way herdr does.
func (f *fakeHerdr) dropTab(t *tabState) string {
	delete(f.tabs, t.id)
	return t.id
}

func (f *fakeHerdr) ExportLayoutForPane(_ context.Context, paneID string) (*herdr.LayoutDescription, error) {
	if err := f.track("export %s", paneID); err != nil {
		return nil, err
	}
	t := f.tabOf(paneID)
	if t == nil {
		return nil, &herdr.APIError{Code: "not_found", Message: "no such pane " + paneID}
	}
	return &herdr.LayoutDescription{
		WorkspaceID:   t.workspaceID,
		TabID:         t.id,
		Zoomed:        t.zoomed,
		FocusedPaneID: f.focused,
		Root:          *toLayout(t.root),
	}, nil
}

func (f *fakeHerdr) Snapshot(_ context.Context) (*herdr.SessionSnapshot, error) {
	if err := f.track("snapshot"); err != nil {
		return nil, err
	}
	snap := &herdr.SessionSnapshot{Version: "0.8.2", Protocol: 21, FocusedPaneID: f.focused}
	workspaces := map[string]bool{}
	for _, id := range f.liveTabs() {
		t := f.tabs[id]
		leaves := t.root.Leaves()
		snap.Tabs = append(snap.Tabs, herdr.TabInfo{
			TabID: t.id, WorkspaceID: t.workspaceID, Label: t.label, PaneCount: len(leaves),
		})
		for _, pane := range leaves {
			snap.Panes = append(snap.Panes, herdr.PaneInfo{
				PaneID: pane, TabID: t.id, WorkspaceID: t.workspaceID,
				Focused: pane == f.focused, TerminalID: "term-" + pane,
			})
		}
		if !workspaces[t.workspaceID] {
			workspaces[t.workspaceID] = true
			snap.Workspaces = append(snap.Workspaces, herdr.WorkspaceInfo{
				WorkspaceID: t.workspaceID, Label: t.workspaceID, ActiveTabID: t.id,
			})
		}
	}
	return snap, nil
}

// fakeArea is the screen the fake draws its tabs in: a wide terminal, with room to
// halve twice either way and still come out even.
var fakeArea = herdr.LayoutRect{Width: 320, Height: 80}

func (f *fakeHerdr) PaneLayout(_ context.Context, paneID string) (*herdr.LayoutSnapshot, error) {
	if err := f.track("layout %s", paneID); err != nil {
		return nil, err
	}
	t := f.tabOf(paneID)
	if t == nil {
		return nil, &herdr.APIError{Code: "not_found", Message: "no such pane " + paneID}
	}
	snap := &herdr.LayoutSnapshot{
		WorkspaceID: t.workspaceID, TabID: t.id, Zoomed: t.zoomed,
		Area: fakeArea, FocusedPaneID: f.focused,
	}
	snap.Panes = paneRects(t.root, fakeArea, f.focused, nil)
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

func (f *fakeHerdr) ResizePane(_ context.Context, paneID string, dir herdr.Direction, amount float64) (*herdr.ResizeResult, error) {
	if err := f.track("resize %s %s", paneID, dir); err != nil {
		return nil, err
	}
	t := f.tabOf(paneID)
	if t == nil {
		return nil, &herdr.APIError{Code: "not_found", Message: "no such pane " + paneID}
	}
	res := &herdr.ResizeResult{PaneID: paneID, FocusedPaneID: f.focused, Reason: "unchanged"}

	if amount == 0 {
		amount = 0.05 // herdr's own default step
	}
	axis := herdr.Right
	if dir == herdr.Up || dir == herdr.DirDn {
		axis = herdr.Down
	}
	if dir == herdr.Left || dir == herdr.Up {
		amount = -amount
	}

	// herdr moves the split nearest the pane along that axis, which in a binary tree
	// is the closest ancestor sharing it — and, since a pane at the edge of the tab
	// has nothing beyond it, the same split whichever way the key pointed.
	path, _ := t.root.Path(paneID)
	for i := len(path); i > 0; i-- {
		at := path[:i-1]
		split := nodeAt(t.root, at)
		if split == nil || split.Dir != axis {
			continue
		}
		before := split.Ratio
		t.root = tree.SetRatioAt(t.root, at, before+amount)
		if nodeAt(t.root, at).Ratio != before {
			res.Changed, res.Reason = true, ""
		}
		break
	}
	return res, nil
}

// nodeAt walks a path from the root, as herdr's own split paths do.
func nodeAt(n *tree.Node, path []bool) *tree.Node {
	for _, second := range path {
		if n == nil || n.IsLeaf() {
			return nil
		}
		if second {
			n = n.Second
		} else {
			n = n.First
		}
	}
	return n
}

func (f *fakeHerdr) SwapDirection(_ context.Context, paneID string, dir herdr.Direction) (*herdr.SwapResult, error) {
	if err := f.track("swap-dir %s %s", paneID, dir); err != nil {
		return nil, err
	}
	t := f.tabOf(paneID)
	if t == nil {
		return &herdr.SwapResult{SourcePaneID: paneID, Reason: "not_found"}, nil
	}
	// Direction-based neighbour lookup needs real geometry; the engine does no
	// tree maths for this call, so the fake only has to report a plausible
	// outcome: a swap succeeds when the tab has someone else in it.
	leaves := t.root.Leaves()
	if len(leaves) < 2 {
		return &herdr.SwapResult{SourcePaneID: paneID, Reason: "no_neighbor"}, nil
	}
	other := leaves[0]
	if other == paneID {
		other = leaves[1]
	}
	t.root = tree.SwapPanes(t.root, paneID, other)
	f.focused = paneID
	return &herdr.SwapResult{Changed: true, SourcePaneID: paneID, TargetPaneID: other, FocusedPaneID: paneID}, nil
}

func (f *fakeHerdr) SwapPanes(_ context.Context, source, target string) (*herdr.SwapResult, error) {
	if err := f.track("swap %s %s", source, target); err != nil {
		return nil, err
	}
	st, tt := f.tabOf(source), f.tabOf(target)
	switch {
	case st == nil || tt == nil:
		return &herdr.SwapResult{SourcePaneID: source, Reason: "not_found"}, nil
	case st != tt:
		return &herdr.SwapResult{SourcePaneID: source, Reason: "cross_tab"}, nil
	case source == target:
		return &herdr.SwapResult{SourcePaneID: source, Reason: "same_pane"}, nil
	}
	st.root = tree.SwapPanes(st.root, source, target)
	f.focused = source
	return &herdr.SwapResult{Changed: true, SourcePaneID: source, TargetPaneID: target, FocusedPaneID: source}, nil
}

func (f *fakeHerdr) MovePane(_ context.Context, paneID string, dest herdr.Destination, focus bool) (*herdr.MoveResult, error) {
	if err := f.track("move %s -> %s", paneID, describeDest(dest)); err != nil {
		return nil, err
	}
	src := f.tabOf(paneID)
	if src == nil {
		return nil, &herdr.APIError{Code: "not_found", Message: "no such pane " + paneID}
	}

	res := &herdr.MoveResult{
		PreviousPaneID:      paneID,
		PreviousTabID:       src.id,
		PreviousWorkspaceID: src.workspaceID,
	}

	// herdr refuses a move whose destination is the pane's own tab: same-tab
	// rearrangement has to go through pane.swap.
	if dest.Type == "tab" && dest.TabID == src.id {
		res.Reason = "same_tab"
		res.Pane = herdr.PaneInfo{PaneID: paneID, TabID: src.id, WorkspaceID: src.workspaceID}
		res.FocusedPaneID = f.focused
		return res, nil
	}
	// ...or one involving a zoomed tab.
	if src.zoomed || (dest.Type == "tab" && f.tabs[dest.TabID] != nil && f.tabs[dest.TabID].zoomed) {
		res.Reason = "zoomed_tab"
		res.Pane = herdr.PaneInfo{PaneID: paneID, TabID: src.id, WorkspaceID: src.workspaceID}
		res.FocusedPaneID = f.focused
		return res, nil
	}

	// Detach.
	src.root = tree.Remove(src.root, paneID)
	if src.root == nil {
		res.ClosedTabID = f.dropTab(src)
	}

	newPaneID, newTab := paneID, (*tabState)(nil)
	switch dest.Type {
	case "tab":
		target, ok := f.tabs[dest.TabID]
		if !ok {
			return nil, &herdr.APIError{Code: "not_found", Message: "no such tab " + dest.TabID}
		}
		host := dest.TargetPaneID
		if host == "" {
			// herdr splits the destination tab's active pane; any deterministic
			// choice is fine for parking, which never names a host.
			host = target.root.FirstLeaf()
		}
		if !target.root.Has(host) {
			return nil, &herdr.APIError{Code: "not_found", Message: "no such pane " + host}
		}
		ratio := 0.5
		if dest.Ratio != nil {
			ratio = tree.Clamp(*dest.Ratio)
		}
		// herdr's split_at makes the existing pane the first child.
		target.root = tree.Insert(target.root, host, paneID, dest.Split, ratio)
		newTab = target

	case "new_tab":
		ws := dest.WorkspaceID
		if ws == "" {
			ws = src.workspaceID
		}
		id := fmt.Sprintf("%s:t%d", ws, f.nextTab)
		f.nextTab++
		newTab = &tabState{id: id, workspaceID: ws, label: dest.Label, root: tree.Leaf(paneID)}
		f.tabs[id] = newTab
		f.tabOrder = append(f.tabOrder, id)
		res.CreatedTab = &herdr.TabInfo{TabID: id, WorkspaceID: ws, Label: dest.Label, PaneCount: 1}

	case "new_workspace":
		ws := fmt.Sprintf("w%d", f.nextWS)
		f.nextWS++
		// Public pane ids are workspace-scoped, so crossing a workspace renames
		// the pane. The engine has to notice.
		newPaneID = fmt.Sprintf("%s:p%d", ws, f.nextPane)
		f.nextPane++
		id := fmt.Sprintf("%s:t1", ws)
		newTab = &tabState{id: id, workspaceID: ws, label: dest.TabLabel, root: tree.Leaf(newPaneID)}
		f.tabs[id] = newTab
		f.tabOrder = append(f.tabOrder, id)
		res.CreatedWorkspace = &herdr.WorkspaceInfo{WorkspaceID: ws, Label: dest.Label, ActiveTabID: id}
		res.CreatedTab = &herdr.TabInfo{TabID: id, WorkspaceID: ws, PaneCount: 1}

	default:
		return nil, fmt.Errorf("fake: unknown destination %q", dest.Type)
	}

	res.Changed = true
	res.Pane = herdr.PaneInfo{
		PaneID: newPaneID, TabID: newTab.id, WorkspaceID: newTab.workspaceID,
		TerminalID: "term-" + paneID, // the terminal survives the move
	}
	if focus {
		f.focused = newPaneID
	}
	res.FocusedPaneID = f.focused
	return res, nil
}

// SplitPane divides a pane's slot in two, the new pane taking the second half —
// herdr's own split_at makes the existing pane the first child, as pane.move does.
func (f *fakeHerdr) SplitPane(_ context.Context, targetPaneID string, split herdr.SplitDirection, ratio float64) (*herdr.PaneInfo, error) {
	if err := f.track("split %s %s %.2f", targetPaneID, split, ratio); err != nil {
		return nil, err
	}
	t := f.tabOf(targetPaneID)
	if t == nil {
		return nil, &herdr.APIError{Code: "not_found", Message: "no such pane " + targetPaneID}
	}
	// A fresh pane, named the way herdr names one: scoped to the workspace it is in.
	id := fmt.Sprintf("%s:p%d", t.workspaceID, f.nextPane)
	f.nextPane++
	t.root = tree.Insert(t.root, targetPaneID, id, split, tree.Clamp(ratio))
	return &herdr.PaneInfo{PaneID: id, TabID: t.id, WorkspaceID: t.workspaceID, TerminalID: "term-" + id}, nil
}

// ClosePane removes a pane, collapsing its slot onto its sibling and closing the
// tab if it was the last one — which is what makes the stand-in trick work.
func (f *fakeHerdr) ClosePane(_ context.Context, paneID string) error {
	if err := f.track("close %s", paneID); err != nil {
		return err
	}
	t := f.tabOf(paneID)
	if t == nil {
		return &herdr.APIError{Code: "not_found", Message: "no such pane " + paneID}
	}
	t.root = tree.Remove(t.root, paneID)
	if t.root == nil {
		f.dropTab(t)
	}
	if f.focused == paneID {
		f.focused = ""
	}
	return nil
}

func (f *fakeHerdr) RenamePane(_ context.Context, paneID, label string) error {
	if err := f.track("rename %s %q", paneID, label); err != nil {
		return err
	}
	if f.tabOf(paneID) == nil {
		return &herdr.APIError{Code: "not_found", Message: "no such pane " + paneID}
	}
	return nil
}

func (f *fakeHerdr) SetSplitRatio(_ context.Context, tabID string, path []bool, ratio float64) error {
	if err := f.track("ratio %s %v = %.2f", tabID, path, ratio); err != nil {
		return err
	}
	t, ok := f.tabs[tabID]
	if !ok {
		return &herdr.APIError{Code: "not_found", Message: "no such tab " + tabID}
	}
	t.root = tree.SetRatioAt(t.root, path, ratio)
	return nil
}

func (f *fakeHerdr) FocusPane(_ context.Context, paneID string) error {
	if err := f.track("focus %s", paneID); err != nil {
		return err
	}
	if f.tabOf(paneID) == nil {
		return &herdr.APIError{Code: "not_found", Message: "no such pane " + paneID}
	}
	f.focused = paneID
	return nil
}

func (f *fakeHerdr) Zoom(_ context.Context, paneID, mode string) (*herdr.ZoomResult, error) {
	if err := f.track("zoom %s %s", paneID, mode); err != nil {
		return nil, err
	}
	t := f.tabOf(paneID)
	if t == nil {
		return nil, &herdr.APIError{Code: "not_found", Message: "no such pane " + paneID}
	}
	was := t.zoomed
	switch mode {
	case "on":
		t.zoomed = true
	case "off":
		t.zoomed = false
	default:
		t.zoomed = !t.zoomed
	}
	return &herdr.ZoomResult{
		Changed: was != t.zoomed, ZoomChanged: was != t.zoomed,
		PaneID: paneID, FocusedPaneID: f.focused, Zoomed: t.zoomed,
	}, nil
}

// toLayout converts back into the wire shape, so the engine goes through the
// same decoding path it uses against the real server.
func toLayout(n *tree.Node) *herdr.LayoutNode {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return &herdr.LayoutNode{Type: "pane", PaneID: n.PaneID}
	}
	return &herdr.LayoutNode{
		Type: "split", Direction: n.Dir, Ratio: n.Ratio,
		First: toLayout(n.First), Second: toLayout(n.Second),
	}
}

func describeDest(d herdr.Destination) string {
	switch d.Type {
	case "tab":
		ratio := "default"
		if d.Ratio != nil {
			ratio = fmt.Sprintf("%.2f", *d.Ratio)
		}
		return fmt.Sprintf("tab %s (host %q, %s, ratio %s)", d.TabID, d.TargetPaneID, d.Split, ratio)
	case "new_tab":
		return fmt.Sprintf("new tab in %s labelled %q", d.WorkspaceID, d.Label)
	default:
		return "new workspace"
	}
}
