// Package engine turns the user's intent into herdr API calls.
//
// It sits between the UI and the socket client: it reads the current tab,
// computes a target tree with package tree, and executes the resulting plan
// while looking after the things a plan cannot express — zoom, focus, and
// recovering panes if a rebuild is interrupted.
package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

// Client is the part of the herdr API the engine needs. *herdr.Client
// implements it; tests substitute a fake.
type Client interface {
	Snapshot(ctx context.Context) (*herdr.SessionSnapshot, error)
	ExportLayoutForPane(ctx context.Context, paneID string) (*herdr.LayoutDescription, error)
	SwapDirection(ctx context.Context, paneID string, dir herdr.Direction) (*herdr.SwapResult, error)
	SwapPanes(ctx context.Context, sourcePaneID, targetPaneID string) (*herdr.SwapResult, error)
	MovePane(ctx context.Context, paneID string, dest herdr.Destination, focus bool) (*herdr.MoveResult, error)
	SetSplitRatio(ctx context.Context, tabID string, path []bool, ratio float64) error
	FocusPane(ctx context.Context, paneID string) error
	Zoom(ctx context.Context, paneID, mode string) (*herdr.ZoomResult, error)
}

// Engine operates on one pane: the pane that was focused when the popup opened.
type Engine struct {
	client   Client
	stateDir string

	// paneID, tabID and workspaceID track the operated-on pane. They change as
	// it moves between tabs and workspaces.
	paneID      string
	tabID       string
	workspaceID string
}

// New returns an engine for a pane. stateDir is the plugin's state directory;
// an empty one disables the parking journal, which only costs crash recovery.
func New(client Client, stateDir, paneID, tabID, workspaceID string) *Engine {
	return &Engine{
		client:      client,
		stateDir:    stateDir,
		paneID:      paneID,
		tabID:       tabID,
		workspaceID: workspaceID,
	}
}

// PaneID returns the pane the engine operates on. It changes when the pane moves
// to another workspace, because herdr's public pane ids are workspace-scoped.
func (e *Engine) PaneID() string { return e.paneID }

// TabID returns the tab the operated-on pane currently lives in.
func (e *Engine) TabID() string { return e.tabID }

// WorkspaceID returns the workspace the operated-on pane currently lives in.
func (e *Engine) WorkspaceID() string { return e.workspaceID }

// ErrPaneGone means the operated-on pane no longer exists: it was closed while
// the popup was open. The UI closes itself rather than guessing.
var ErrPaneGone = errors.New("the pane is gone")

// Tab is a snapshot of the tab the operated-on pane lives in.
type Tab struct {
	// Layout is what herdr reported.
	Layout *herdr.LayoutDescription
	// Tree is Layout.Root in the planner's model.
	Tree *tree.Node
	// Preset is the layout the tab currently matches, if any.
	Preset    tree.Preset
	HasPreset bool
}

// PaneCount returns how many panes the tab holds.
func (t *Tab) PaneCount() int { return t.Tree.Count() }

// LayoutName describes the tab's layout for the status line.
func (t *Tab) LayoutName() string {
	if t.HasPreset {
		return t.Preset.Name()
	}
	return "custom"
}

// Tab reads the current state of the operated-on pane's tab.
//
// Every operation re-reads first: a pane can be closed, or a tab rearranged from
// elsewhere, while the popup sits open.
func (e *Engine) Tab(ctx context.Context) (*Tab, error) {
	layout, err := e.client.ExportLayoutForPane(ctx, e.paneID)
	if err != nil {
		// herdr reports an unknown pane as a not_found error; treat that as the
		// pane having been closed rather than as a transport failure.
		if herdr.Code(err) == "not_found" {
			return nil, fmt.Errorf("%s: %w", e.paneID, ErrPaneGone)
		}
		return nil, err
	}
	node := tree.FromLayout(&layout.Root)
	if node == nil || !node.Has(e.paneID) {
		return nil, fmt.Errorf("%s: %w", e.paneID, ErrPaneGone)
	}

	e.tabID = layout.TabID
	e.workspaceID = layout.WorkspaceID

	t := &Tab{Layout: layout, Tree: node}
	// Detect against the operated-on pane, so the name the UI shows is the one a
	// number key would reproduce.
	t.Preset, t.HasPreset = tree.DetectFor(node, e.paneID)
	return t, nil
}

// Snapshot reads the whole session, for the tree view.
func (e *Engine) Snapshot(ctx context.Context) (*herdr.SessionSnapshot, error) {
	return e.client.Snapshot(ctx)
}

// Swap exchanges the pane with its neighbour in a direction, keeping the tab's
// shape. Returns tree.ErrNoChange when there is no neighbour that way.
func (e *Engine) Swap(ctx context.Context, dir herdr.Direction) error {
	res, err := e.client.SwapDirection(ctx, e.paneID, dir)
	if err != nil {
		return err
	}
	if !res.Changed {
		return fmt.Errorf("no pane to the %s: %w", dir, tree.ErrNoChange)
	}
	return nil
}

// ReSplit moves the pane to one side of a larger region of the tab. See
// tree.ReSplit for the exact semantics.
func (e *Engine) ReSplit(ctx context.Context, dir herdr.Direction) error {
	t, err := e.Tab(ctx)
	if err != nil {
		return err
	}
	want, err := tree.ReSplit(t.Tree, e.paneID, dir)
	if err != nil {
		return err
	}
	return e.Reshape(ctx, t, want)
}

// ApplyPreset lays the tab out according to a preset, with the operated-on pane
// as the main one.
func (e *Engine) ApplyPreset(ctx context.Context, preset tree.Preset) error {
	t, err := e.Tab(ctx)
	if err != nil {
		return err
	}
	want := preset.Build(t.Tree.Leaves(), e.paneID)
	return e.Reshape(ctx, t, want)
}

// CyclePreset applies the next preset that would change the tab, and reports
// which one it used.
func (e *Engine) CyclePreset(ctx context.Context) (tree.Preset, error) {
	t, err := e.Tab(ctx)
	if err != nil {
		return 0, err
	}
	preset := tree.Next(t.Tree, e.paneID)
	want := preset.Build(t.Tree.Leaves(), e.paneID)
	return preset, e.Reshape(ctx, t, want)
}

// Balance evens out the pane sizes without touching the layout: every split is
// re-weighted so the panes sharing an axis get the same room. See tree.Balance.
//
// It reports whether the sizes came out exactly even, which herdr's ratio clamp
// can prevent.
func (e *Engine) Balance(ctx context.Context) (exact bool, err error) {
	t, err := e.Tab(ctx)
	if err != nil {
		return false, err
	}
	return tree.BalanceIsExact(t.Tree), e.Reshape(ctx, t, tree.Balance(t.Tree))
}

// Reshape makes the tab match want. It is the single path every structural
// change takes.
func (e *Engine) Reshape(ctx context.Context, t *Tab, want *tree.Node) error {
	steps, err := tree.Plan(t.Tree, want)
	if err != nil {
		return err
	}
	if len(steps) == 0 {
		return tree.ErrNoChange
	}
	return e.execute(ctx, t, steps)
}

// MoveToNewTab moves the pane into a fresh tab. An empty workspaceID means its
// current workspace.
func (e *Engine) MoveToNewTab(ctx context.Context, workspaceID string) error {
	return e.moveSelf(ctx, herdr.DestNewTab(workspaceID, ""))
}

// MoveToNewWorkspace moves the pane into a fresh workspace.
func (e *Engine) MoveToNewWorkspace(ctx context.Context) error {
	return e.moveSelf(ctx, herdr.DestNewWorkspace("", ""))
}

// MoveToTab moves the pane into an existing tab, splitting its active pane
// downwards.
func (e *Engine) MoveToTab(ctx context.Context, tabID string) error {
	return e.MoveToTabBeside(ctx, tabID, "")
}

// MoveToTabBeside moves the pane into an existing tab, splitting a named pane of
// that tab downwards. An empty targetPaneID lets herdr pick the tab's active pane.
//
// This is how the tree view offers panes in other tabs as destinations: pane.swap
// cannot cross a tab boundary, so landing next to the chosen pane is the closest
// thing to a cross-tab swap that keeps the terminal alive.
func (e *Engine) MoveToTabBeside(ctx context.Context, tabID, targetPaneID string) error {
	if tabID == e.tabID {
		return tree.ErrNoChange
	}
	return e.moveSelf(ctx, herdr.DestTab(tabID, targetPaneID, herdr.Down, nil))
}

// SwapWithPane exchanges the pane with a named pane. Both must be in the same
// tab, which is why the tree view only offers this for same-tab panes.
func (e *Engine) SwapWithPane(ctx context.Context, targetPaneID string) error {
	if targetPaneID == e.paneID {
		return tree.ErrNoChange
	}
	res, err := e.client.SwapPanes(ctx, e.paneID, targetPaneID)
	if err != nil {
		return err
	}
	if !res.Changed {
		return fmt.Errorf("cannot swap with %s (%s): %w", targetPaneID, res.Reason, tree.ErrNoChange)
	}
	return nil
}

// moveSelf moves the operated-on pane and re-reads its identity, which changes
// when it crosses a workspace boundary.
func (e *Engine) moveSelf(ctx context.Context, dest herdr.Destination) error {
	res, err := e.client.MovePane(ctx, e.paneID, dest, true)
	if err != nil {
		return err
	}
	if !res.Changed {
		return fmt.Errorf("pane not moved (%s): %w", res.Reason, tree.ErrNoChange)
	}
	e.paneID = res.Pane.PaneID
	e.tabID = res.Pane.TabID
	e.workspaceID = res.Pane.WorkspaceID
	return nil
}

// execute runs a plan.
//
// A plan that parks panes needs care beyond the steps themselves: herdr refuses
// to move panes out of a zoomed tab, focus must end up back on the operated-on
// pane, and an interruption must leave a trail for recovery.
func (e *Engine) execute(ctx context.Context, t *Tab, steps []tree.Step) error {
	tabID := t.Layout.TabID
	parks := false
	for _, s := range steps {
		if s.Kind == tree.StepPark {
			parks = true
			break
		}
	}

	// pane.move refuses a zoomed tab, so drop the zoom and put it back after.
	// pane.swap works fine while zoomed, so a swap-only plan is left alone.
	if parks && t.Layout.Zoomed {
		if _, err := e.client.Zoom(ctx, e.paneID, "off"); err != nil {
			return fmt.Errorf("unzoom before rebuild: %w", err)
		}
		defer func() {
			// Best effort: the rebuild is what the user asked for, and failing to
			// restore zoom should not mask its result.
			_, _ = e.client.Zoom(ctx, e.paneID, "on")
		}()
	}

	var journal *Journal
	scratchTabID := ""

	for i, s := range steps {
		var err error
		switch s.Kind {
		case tree.StepSwap:
			_, err = e.client.SwapPanes(ctx, s.PaneID, s.Target)

		case tree.StepSetRatio:
			err = e.client.SetSplitRatio(ctx, tabID, s.Path, s.Ratio)

		case tree.StepPark:
			if scratchTabID == "" {
				journal = newJournal(e.stateDir, t.Layout.WorkspaceID, tabID)
				var res *herdr.MoveResult
				res, err = e.client.MovePane(ctx, s.PaneID, herdr.DestNewTab(t.Layout.WorkspaceID, ParkingLabel), false)
				if err == nil {
					scratchTabID = res.Pane.TabID
					journal.ScratchTabID = scratchTabID
					err = journal.add(s.PaneID)
				}
			} else {
				_, err = e.client.MovePane(ctx, s.PaneID, herdr.DestTab(scratchTabID, "", herdr.Down, nil), false)
				if err == nil {
					err = journal.add(s.PaneID)
				}
			}

		case tree.StepInsert:
			ratio := s.Ratio
			_, err = e.client.MovePane(ctx, s.PaneID, herdr.DestTab(tabID, s.Target, s.Split, &ratio), false)
			if err == nil && journal != nil {
				err = journal.done(s.PaneID)
			}
		}

		if err != nil {
			return e.abort(ctx, journal, tabID, fmt.Errorf("step %d of %d (%s): %w", i+1, len(steps), s, err))
		}
	}

	if journal != nil {
		if err := journal.clear(); err != nil {
			return err
		}
	}

	// Every move was made with focus off, so herdr's focus may be anywhere. Put
	// it back on the pane the user is arranging.
	if err := e.client.FocusPane(ctx, e.paneID); err != nil {
		return fmt.Errorf("restore focus: %w", err)
	}
	return nil
}

// abort tries to get parked panes back into their tab after a failed step, so a
// failure never leaves a terminal hidden in a scratch tab.
//
// It returns the original error, with any recovery trouble appended: the caller
// needs to hear about the failure either way.
func (e *Engine) abort(ctx context.Context, journal *Journal, tabID string, cause error) error {
	if journal == nil || len(journal.Panes) == 0 {
		return cause
	}

	stranded := append([]string(nil), journal.Panes...)
	for _, pane := range stranded {
		if _, err := e.client.MovePane(ctx, pane, herdr.DestTab(tabID, "", herdr.Down, nil), false); err != nil {
			return fmt.Errorf("%w (and %d pane(s) are still in the %q tab; they will be recovered on restart)",
				cause, len(journal.Panes), ParkingLabel)
		}
		if err := journal.done(pane); err != nil {
			return fmt.Errorf("%w (recovered the panes, but could not update the journal: %v)", cause, err)
		}
	}
	if err := journal.clear(); err != nil {
		return fmt.Errorf("%w (recovered the panes, but could not clear the journal: %v)", cause, err)
	}
	_ = e.client.FocusPane(ctx, e.paneID)
	return fmt.Errorf("%w (the panes were put back)", cause)
}

// Drain recovers panes left in a scratch tab by an interrupted rebuild. It is
// wired to the plugin's startup hook, and does nothing when there is no journal.
//
// It reports how many panes it moved.
func Drain(ctx context.Context, client Client, stateDir string) (int, error) {
	journal, err := loadJournal(stateDir)
	if err != nil {
		return 0, err
	}
	if journal == nil || len(journal.Panes) == 0 {
		if journal != nil {
			return 0, journal.clear()
		}
		return 0, nil
	}

	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		return 0, err
	}

	// The pane may have been closed, or moved by hand, since the journal was
	// written; only touch panes that are still sitting in the scratch tab.
	inScratch := map[string]bool{}
	for _, p := range snapshot.Panes {
		if p.TabID == journal.ScratchTabID {
			inScratch[p.PaneID] = true
		}
	}
	homeExists := false
	for _, tab := range snapshot.Tabs {
		if tab.TabID == journal.HomeTabID {
			homeExists = true
			break
		}
	}

	// If the tab they came from is gone, a new tab in the same workspace is the
	// safest home: it keeps the panes visible without guessing at a layout.
	dest := herdr.DestTab(journal.HomeTabID, "", herdr.Down, nil)
	if !homeExists {
		dest = herdr.DestNewTab(journal.WorkspaceID, "arrange:recovered")
	}

	moved := 0
	for _, pane := range append([]string(nil), journal.Panes...) {
		if !inScratch[pane] {
			if err := journal.done(pane); err != nil {
				return moved, err
			}
			continue
		}
		res, err := client.MovePane(ctx, pane, dest, false)
		if err != nil {
			return moved, fmt.Errorf("recover pane %s: %w", pane, err)
		}
		moved++
		// Once one pane has made a new tab, the rest join it rather than each
		// making another.
		if dest.Type == "new_tab" {
			dest = herdr.DestTab(res.Pane.TabID, "", herdr.Down, nil)
		}
		if err := journal.done(pane); err != nil {
			return moved, err
		}
	}
	return moved, journal.clear()
}
