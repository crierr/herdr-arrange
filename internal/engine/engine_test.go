package engine

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

func r(first, second *tree.Node) *tree.Node {
	return tree.Split(herdr.Right, 0.5, first, second)
}

func d(first, second *tree.Node) *tree.Node {
	return tree.Split(herdr.Down, 0.5, first, second)
}

func leaf(id string) *tree.Node { return tree.Leaf(id) }

// setup returns a fake herdr holding one tab, and an engine pointed at a pane
// in it. stateDir is a real directory so the parking journal is exercised.
func setup(t *testing.T, root *tree.Node, pane string) (*fakeHerdr, *Engine, string) {
	t.Helper()
	fake := newFakeHerdr().withTab("w1", root)
	stateDir := t.TempDir()
	tab := fake.tabOf(pane)
	if tab == nil {
		t.Fatalf("pane %s is not in the seeded tab", pane)
	}
	return fake, New(fake, stateDir, pane, tab.id, tab.workspaceID), stateDir
}

func TestTabReadsTheCurrentLayout(t *testing.T) {
	fake, eng, _ := setup(t, r(leaf("p1"), d(leaf("p2"), leaf("p3"))), "p2")

	tab, err := eng.Tab(context.Background())
	if err != nil {
		t.Fatalf("Tab: %v", err)
	}
	if tab.Tree.String() != "[p1 | [p2 / p3]]" {
		t.Errorf("Tree = %s", tab.Tree)
	}
	if tab.PaneCount() != 3 {
		t.Errorf("PaneCount = %d", tab.PaneCount())
	}
	if eng.TabID() != fake.liveTabs()[0] || eng.WorkspaceID() != "w1" {
		t.Errorf("engine tracked %s/%s", eng.WorkspaceID(), eng.TabID())
	}
	// [p1 | [p2 / p3]] is main-vertical for p1, but the engine is arranging p2, and
	// naming it main-vertical would promise something ApplyPreset would change.
	if tab.HasPreset {
		t.Errorf("LayoutName = %q, want custom for a pane that is not the main one", tab.LayoutName())
	}

	// The same shape, read from the pane in the main slot.
	_, eng, _ = setup(t, r(leaf("p1"), d(leaf("p2"), leaf("p3"))), "p1")
	tab, err = eng.Tab(context.Background())
	if err != nil {
		t.Fatalf("Tab: %v", err)
	}
	if !tab.HasPreset || tab.LayoutName() != "main-vertical" {
		t.Errorf("LayoutName = %q, want main-vertical", tab.LayoutName())
	}
}

func TestTabReportsAClosedPane(t *testing.T) {
	fake := newFakeHerdr().withTab("w1", r(leaf("p1"), leaf("p2")))
	eng := New(fake, t.TempDir(), "gone", "w1:t1", "w1")

	_, err := eng.Tab(context.Background())
	if !errors.Is(err, ErrPaneGone) {
		t.Errorf("err = %v, want ErrPaneGone", err)
	}
}

func TestApplyPresetRestructuresTheTab(t *testing.T) {
	// Start from a shape no preset matches, so this must go through a rebuild.
	fake, eng, stateDir := setup(t, r(r(leaf("p1"), leaf("p2")), leaf("p3")), "p2")

	if err := eng.ApplyPreset(context.Background(), tree.MainVertical); err != nil {
		t.Fatalf("ApplyPreset: %v", err)
	}

	// p2 is the pane being arranged, so main-vertical makes it the big left column.
	got := fake.treeOf(eng.TabID())
	want := tree.MainVertical.Build([]string{"p2", "p1", "p3"}, "p2")
	if !tree.Equal(got, want) {
		t.Fatalf("tab = %#v, want %#v\ncalls:\n%s", got, want, fake.callLog())
	}
	if len(fake.liveTabs()) != 1 {
		t.Errorf("the scratch tab was not cleaned up: %v", fake.liveTabs())
	}
	if fake.focused != "p2" {
		t.Errorf("focus = %s, want the arranged pane p2", fake.focused)
	}
	assertNoJournal(t, stateDir)
}

func TestApplyPresetKeepsEveryPane(t *testing.T) {
	// The whole point of the park-and-reinsert approach is that no terminal is
	// ever destroyed.
	fake, eng, _ := setup(t, r(leaf("p1"), r(leaf("p2"), r(leaf("p3"), leaf("p4")))), "p3")

	for _, preset := range tree.Presets {
		if err := eng.ApplyPreset(context.Background(), preset); err != nil && !errors.Is(err, tree.ErrNoChange) {
			t.Fatalf("%s: %v", preset.Name(), err)
		}
		panes := fake.allPanes()
		if len(panes) != 4 {
			t.Fatalf("%s: session holds %v, want four panes\ncalls:\n%s", preset.Name(), panes, fake.callLog())
		}
	}
}

func TestReSplitGoesThroughARebuild(t *testing.T) {
	fake, eng, _ := setup(t, r(leaf("p1"), d(leaf("p2"), leaf("p3"))), "p2")

	if err := eng.ReSplit(context.Background(), herdr.Left); err != nil {
		t.Fatalf("ReSplit: %v", err)
	}
	if got := fake.treeOf(eng.TabID()).String(); got != "[p1 | [p2 | p3]]" {
		t.Errorf("tab = %s, want [p1 | [p2 | p3]]\ncalls:\n%s", got, fake.callLog())
	}
}

func TestReSplitAtTheTabEdgeChangesNothing(t *testing.T) {
	fake, eng, _ := setup(t, r(leaf("p1"), leaf("p2")), "p1")

	err := eng.ReSplit(context.Background(), herdr.Left)
	if !errors.Is(err, tree.ErrNoChange) {
		t.Errorf("err = %v, want ErrNoChange", err)
	}
	if got := fake.treeOf(eng.TabID()).String(); got != "[p1 | p2]" {
		t.Errorf("tab = %s, should be untouched", got)
	}
}

func TestEvenResizesATabThatIsAlreadyOneAxis(t *testing.T) {
	// A row of columns is already the right shape, so evening it out is ratios
	// only: no pane.move, which is what keeps it free of flicker.
	fake, eng, _ := setup(t, tree.Split(herdr.Right, 0.8, leaf("p1"),
		tree.Split(herdr.Right, 0.9, leaf("p2"), leaf("p3"))), "p1")

	res, err := eng.Even(context.Background())
	if err != nil {
		t.Fatalf("Even: %v", err)
	}
	if res.Reshaped || !res.Exact || res.Dir != herdr.Right || res.Panes != 3 {
		t.Errorf("result = %#v, want three exact columns without a reshape", res)
	}
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "move ") {
			t.Errorf("Even moved a pane: %s", call)
		}
	}
	// A third each: the root splits one pane off from two.
	got := fake.treeOf(eng.TabID())
	if abs(got.Ratio-1.0/3.0) > 0.01 || abs(got.Second.Ratio-0.5) > 0.01 {
		t.Errorf("ratios = %#v", got)
	}
}

func TestEvenRebuildsATabThatMixesAxes(t *testing.T) {
	// No set of ratios makes the panes of [p1 | [p2 / p3]] the same size, so this
	// is the case where `e` rebuilds the tab as a plain row — and says so.
	fake, eng, _ := setup(t, tree.Split(herdr.Right, 0.8, leaf("p1"),
		tree.Split(herdr.Down, 0.9, leaf("p2"), leaf("p3"))), "p1")

	res, err := eng.Even(context.Background())
	if err != nil {
		t.Fatalf("Even: %v", err)
	}
	if !res.Reshaped || !res.Exact || res.Dir != herdr.Right || res.Panes != 3 {
		t.Errorf("result = %#v, want three exact columns via a reshape", res)
	}
	got := fake.treeOf(eng.TabID())
	if got.String() != "[p1 | [p2 | p3]]" {
		t.Fatalf("tab = %s, want three columns\ncalls:\n%s", got, fake.callLog())
	}
	if abs(got.Ratio-1.0/3.0) > 0.01 || abs(got.Second.Ratio-0.5) > 0.01 {
		t.Errorf("ratios = %#v", got)
	}
	if len(fake.liveTabs()) != 1 {
		t.Errorf("the scratch tab was not cleaned up: %v", fake.liveTabs())
	}
}

func TestEvenOfAColumnStaysAColumn(t *testing.T) {
	// The axis comes from the root split, so a stack of rows evens out as rows
	// rather than being turned on its side.
	_, eng, _ := setup(t, tree.Split(herdr.Down, 0.8, leaf("p1"),
		tree.Split(herdr.Right, 0.5, leaf("p2"), leaf("p3"))), "p1")

	res, err := eng.Even(context.Background())
	if err != nil {
		t.Fatalf("Even: %v", err)
	}
	if res.Dir != herdr.Down || !res.Reshaped {
		t.Errorf("result = %#v, want a reshape into rows", res)
	}
}

func TestCyclePresetKeepsChangingTheLayout(t *testing.T) {
	fake, eng, _ := setup(t, tree.EvenHorizontal.Build([]string{"p1", "p2", "p3", "p4"}, "p1"), "p1")

	seen := map[string]bool{}
	for press := 1; press <= 6; press++ {
		before := fake.treeOf(eng.TabID()).String()
		preset, err := eng.CyclePreset(context.Background())
		if err != nil {
			t.Fatalf("press %d (%s): %v", press, preset.Name(), err)
		}
		after := fake.treeOf(eng.TabID()).String()
		if before == after {
			t.Fatalf("press %d chose %s, which changed nothing (%s)", press, preset.Name(), before)
		}
		seen[after] = true
	}
	if len(seen) < 4 {
		t.Errorf("six presses produced only %d distinct layouts", len(seen))
	}
}

func TestSwapUsesPaneSwapAndReportsNoNeighbour(t *testing.T) {
	fake, eng, _ := setup(t, r(leaf("p1"), leaf("p2")), "p1")
	if err := eng.Swap(context.Background(), herdr.DirRt); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	if got := fake.treeOf(eng.TabID()).String(); got != "[p2 | p1]" {
		t.Errorf("tab = %s, want [p2 | p1]", got)
	}

	// A single-pane tab has no neighbour in any direction.
	single, lone, _ := setup(t, leaf("only"), "only")
	err := lone.Swap(context.Background(), herdr.Left)
	if !errors.Is(err, tree.ErrNoChange) {
		t.Errorf("err = %v, want ErrNoChange", err)
	}
	if got := single.treeOf(lone.TabID()).String(); got != "only" {
		t.Errorf("tab = %s", got)
	}
}

func TestMoveToNewTabTracksTheNewTab(t *testing.T) {
	fake, eng, _ := setup(t, r(leaf("p1"), leaf("p2")), "p2")
	oldTab := eng.TabID()

	if err := eng.MoveToNewTab(context.Background(), ""); err != nil {
		t.Fatalf("MoveToNewTab: %v", err)
	}
	if eng.TabID() == oldTab {
		t.Error("the engine still points at the old tab")
	}
	if eng.WorkspaceID() != "w1" {
		t.Errorf("workspace = %s, want w1", eng.WorkspaceID())
	}
	if got := fake.treeOf(eng.TabID()).String(); got != "p2" {
		t.Errorf("new tab = %s, want just p2", got)
	}
	if got := fake.treeOf(oldTab).String(); got != "p1" {
		t.Errorf("old tab = %s, want just p1", got)
	}
}

func TestMoveToNewWorkspaceAdoptsTheNewPaneID(t *testing.T) {
	// Public pane ids are workspace-scoped, so this move renames the pane. An
	// engine that kept the old id would then operate on nothing.
	fake, eng, _ := setup(t, r(leaf("p1"), leaf("p2")), "p2")

	if err := eng.MoveToNewWorkspace(context.Background()); err != nil {
		t.Fatalf("MoveToNewWorkspace: %v", err)
	}
	if eng.PaneID() == "p2" {
		t.Error("the engine kept the old pane id across a workspace move")
	}
	if eng.WorkspaceID() == "w1" {
		t.Errorf("workspace = %s, want a new one", eng.WorkspaceID())
	}
	if fake.tabOf(eng.PaneID()) == nil {
		t.Errorf("pane %s does not exist", eng.PaneID())
	}
	// And the engine can still read its tab afterwards.
	if _, err := eng.Tab(context.Background()); err != nil {
		t.Errorf("Tab after the move: %v", err)
	}
}

func TestMoveToTabRefusesTheTabItIsAlreadyIn(t *testing.T) {
	fake, eng, _ := setup(t, r(leaf("p1"), leaf("p2")), "p2")
	before := len(fake.calls)

	if err := eng.MoveToTab(context.Background(), eng.TabID()); !errors.Is(err, tree.ErrNoChange) {
		t.Errorf("err = %v, want ErrNoChange", err)
	}
	if len(fake.calls) != before {
		t.Errorf("a no-op move should not call the API: %v", fake.calls[before:])
	}
}

func TestMoveToTabMovesAcrossWorkspaces(t *testing.T) {
	fake := newFakeHerdr().withTab("w1", r(leaf("p1"), leaf("p2")))
	fake.withTab("w2", leaf("q1"))
	eng := New(fake, t.TempDir(), "p2", "w1:t1", "w1")

	if err := eng.MoveToTab(context.Background(), "w2:t2"); err != nil {
		t.Fatalf("MoveToTab: %v", err)
	}
	if eng.WorkspaceID() != "w2" {
		t.Errorf("workspace = %s, want w2", eng.WorkspaceID())
	}
	if got := fake.treeOf("w2:t2").String(); got != "[q1 / p2]" {
		t.Errorf("destination tab = %s, want [q1 / p2]", got)
	}
}

func TestSwapWithPane(t *testing.T) {
	fake, eng, _ := setup(t, r(leaf("p1"), d(leaf("p2"), leaf("p3"))), "p1")

	if err := eng.SwapWithPane(context.Background(), "p3"); err != nil {
		t.Fatalf("SwapWithPane: %v", err)
	}
	if got := fake.treeOf(eng.TabID()).String(); got != "[p3 | [p2 / p1]]" {
		t.Errorf("tab = %s, want [p3 | [p2 / p1]]", got)
	}

	if err := eng.SwapWithPane(context.Background(), eng.PaneID()); !errors.Is(err, tree.ErrNoChange) {
		t.Errorf("swapping with itself: err = %v, want ErrNoChange", err)
	}
}

func TestRebuildUnzoomsAndRestoresZoom(t *testing.T) {
	// pane.move refuses a zoomed tab outright, so a rebuild has to drop the zoom
	// and put it back.
	fake, eng, _ := setup(t, r(r(leaf("p1"), leaf("p2")), leaf("p3")), "p2")
	fake.tabs[eng.TabID()].zoomed = true

	if err := eng.ApplyPreset(context.Background(), tree.EvenVertical); err != nil {
		t.Fatalf("ApplyPreset: %v", err)
	}
	if !fake.tabs[eng.TabID()].zoomed {
		t.Error("zoom was not restored")
	}
	log := fake.callLog()
	if !strings.Contains(log, "zoom p2 off") || !strings.Contains(log, "zoom p2 on") {
		t.Errorf("expected zoom off then on:\n%s", log)
	}
	want := tree.EvenVertical.Build([]string{"p1", "p2", "p3"}, "p2")
	if got := fake.treeOf(eng.TabID()); !tree.Equal(got, want) {
		t.Errorf("tab = %#v, want %#v", got, want)
	}
}

func TestSwapOnlyPlanLeavesZoomAlone(t *testing.T) {
	// pane.swap works fine on a zoomed tab, so a permutation should not disturb it.
	base := tree.EvenHorizontal.Build([]string{"p1", "p2", "p3"}, "p1")
	fake, eng, _ := setup(t, base, "p1")
	fake.tabs[eng.TabID()].zoomed = true

	tab, err := eng.Tab(context.Background())
	if err != nil {
		t.Fatalf("Tab: %v", err)
	}
	want := tree.EvenHorizontal.Build([]string{"p3", "p2", "p1"}, "p3")
	if err := eng.Reshape(context.Background(), tab, want); err != nil {
		t.Fatalf("Reshape: %v", err)
	}
	if strings.Contains(fake.callLog(), "zoom ") {
		t.Errorf("a swap-only plan should not touch zoom:\n%s", fake.callLog())
	}
	if !fake.tabs[eng.TabID()].zoomed {
		t.Error("the tab is no longer zoomed")
	}
}

func TestInterruptedRebuildPutsThePanesBack(t *testing.T) {
	// If a step fails partway through, the panes already parked must not be left
	// hidden in a scratch tab.
	base := r(r(leaf("p1"), leaf("p2")), r(leaf("p3"), leaf("p4")))
	fake, eng, stateDir := setup(t, base, "p2")

	// Count the calls a clean run takes, then fail in the middle of the next one.
	if err := eng.ApplyPreset(context.Background(), tree.EvenVertical); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	total := fake.callCount

	fake2, eng2, stateDir2 := setup(t, base.Clone(), "p2")
	fake2.failAfter = total / 2

	err := eng2.ApplyPreset(context.Background(), tree.Tiled)
	if err == nil {
		t.Fatal("want the injected failure to surface")
	}
	if !strings.Contains(err.Error(), "injected failure") {
		t.Errorf("err = %v", err)
	}

	// Whatever happened, all four panes must still exist and be reachable.
	if panes := fake2.allPanes(); len(panes) != 4 {
		t.Errorf("session holds %v, want four panes\ncalls:\n%s", panes, fake2.callLog())
	}
	// Either the panes were put back and the journal cleared, or the journal
	// survives so `arrange drain` can finish the job. Never neither.
	journal, jerr := loadJournal(stateDir2)
	if jerr != nil {
		t.Fatalf("loadJournal: %v", jerr)
	}
	if journal != nil && len(journal.Panes) > 0 {
		if journal.ScratchTabID == "" {
			t.Error("the journal names no scratch tab, so drain cannot recover")
		}
	} else if len(fake2.liveTabs()) != 1 {
		t.Errorf("no journal left, but tabs are %v: panes are stranded", fake2.liveTabs())
	}
	assertNoJournal(t, stateDir)
}

func TestInterruptedRebuildLeavesADrainableJournal(t *testing.T) {
	// Fail on the very first insert: the panes are all parked, and the inline
	// recovery has to move them home.
	base := r(r(leaf("p1"), leaf("p2")), r(leaf("p3"), leaf("p4")))
	fake, eng, stateDir := setup(t, base, "p2")

	// export(1) park(2,3,4 with journal writes) then the first insert.
	fake.failAfter = 5

	if err := eng.ApplyPreset(context.Background(), tree.EvenVertical); err == nil {
		t.Fatal("want an error")
	}
	if panes := fake.allPanes(); len(panes) != 4 {
		t.Fatalf("session holds %v, want four panes\ncalls:\n%s", panes, fake.callLog())
	}
	// Recovery moved them home, so the scratch tab is gone and so is the journal.
	if len(fake.liveTabs()) != 1 {
		t.Errorf("tabs = %v, want just the home tab\ncalls:\n%s", fake.liveTabs(), fake.callLog())
	}
	assertNoJournal(t, stateDir)
}

func TestDrainRecoversStrandedPanes(t *testing.T) {
	// Simulate a crash: park panes by hand, write a journal, then drain.
	fake := newFakeHerdr().withTab("w1", leaf("p1"))
	fake.withTab("w1", r(leaf("p2"), leaf("p3")))
	stateDir := t.TempDir()

	home, scratch := "w1:t1", "w1:t2"
	fake.tabs[scratch].label = ParkingLabel
	journal := newJournal(stateDir, "w1", home)
	journal.ScratchTabID = scratch
	if err := journal.add("p2"); err != nil {
		t.Fatal(err)
	}
	if err := journal.add("p3"); err != nil {
		t.Fatal(err)
	}

	moved, err := Drain(context.Background(), fake, stateDir)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if moved != 2 {
		t.Errorf("moved = %d, want 2", moved)
	}
	if got := fake.treeOf(home).Count(); got != 3 {
		t.Errorf("home tab holds %d panes, want 3 (%s)", got, fake.treeOf(home))
	}
	if len(fake.liveTabs()) != 1 {
		t.Errorf("tabs = %v, want just the home tab", fake.liveTabs())
	}
	assertNoJournal(t, stateDir)
}

func TestDrainMakesANewTabWhenHomeIsGone(t *testing.T) {
	// The tab the panes came from may have been closed while the server was down.
	fake := newFakeHerdr().withTab("w1", r(leaf("p2"), leaf("p3")))
	stateDir := t.TempDir()

	journal := newJournal(stateDir, "w1", "w1:t99")
	journal.ScratchTabID = "w1:t1"
	if err := journal.add("p2"); err != nil {
		t.Fatal(err)
	}
	if err := journal.add("p3"); err != nil {
		t.Fatal(err)
	}

	moved, err := Drain(context.Background(), fake, stateDir)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if moved != 2 {
		t.Errorf("moved = %d, want 2", moved)
	}
	// Both panes end up together in one recovered tab, not one tab each.
	if tabs := fake.liveTabs(); len(tabs) != 1 {
		t.Errorf("tabs = %v, want a single recovered tab\ncalls:\n%s", tabs, fake.callLog())
	}
	if panes := fake.allPanes(); len(panes) != 2 {
		t.Errorf("panes = %v, want both recovered", panes)
	}
	assertNoJournal(t, stateDir)
}

func TestDrainSkipsPanesThatMovedOrClosed(t *testing.T) {
	// The journal is a hint, not the truth: a pane may have been closed, or moved
	// by hand, before drain runs.
	fake := newFakeHerdr().withTab("w1", leaf("p1"))
	fake.withTab("w1", leaf("p2"))
	stateDir := t.TempDir()

	journal := newJournal(stateDir, "w1", "w1:t1")
	journal.ScratchTabID = "w1:t2"
	if err := journal.add("p2"); err != nil {
		t.Fatal(err)
	}
	if err := journal.add("closed-since"); err != nil {
		t.Fatal(err)
	}

	moved, err := Drain(context.Background(), fake, stateDir)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if moved != 1 {
		t.Errorf("moved = %d, want 1", moved)
	}
	assertNoJournal(t, stateDir)
}

func TestDrainIsANoOpWithoutAJournal(t *testing.T) {
	fake := newFakeHerdr().withTab("w1", leaf("p1"))
	moved, err := Drain(context.Background(), fake, t.TempDir())
	if err != nil || moved != 0 {
		t.Errorf("Drain = %d, %v; want 0, nil", moved, err)
	}
	// And with no state directory at all, which is how it runs outside herdr.
	if moved, err = Drain(context.Background(), fake, ""); err != nil || moved != 0 {
		t.Errorf("Drain = %d, %v; want 0, nil", moved, err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("Drain called the API with nothing to do: %v", fake.calls)
	}
}

func TestDrainDiscardsACorruptJournal(t *testing.T) {
	stateDir := t.TempDir()
	if err := os.WriteFile(JournalPath(stateDir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := newFakeHerdr().withTab("w1", leaf("p1"))

	if _, err := Drain(context.Background(), fake, stateDir); err == nil {
		t.Error("want an error mentioning the corrupt journal")
	}
	// It must not be left behind to fail again on every start.
	assertNoJournal(t, stateDir)
}

// TestReshapeReachesTheTargetTree drives random targets through the real
// executor against the modelled server: the tab must end up exactly as planned,
// with no panes lost and no scratch tabs left over.
func TestReshapeReachesTheTargetTree(t *testing.T) {
	g := rand.New(rand.NewSource(11))

	for i := range 400 {
		n := 1 + g.Intn(7)
		panes := make([]string, n)
		for j := range panes {
			panes[j] = paneName(j)
		}

		start := randomTree(g, panes)
		fake, eng, stateDir := setup(t, start, panes[g.Intn(n)])

		shuffled := append([]string(nil), panes...)
		g.Shuffle(len(shuffled), func(a, b int) { shuffled[a], shuffled[b] = shuffled[b], shuffled[a] })
		want := randomTree(g, shuffled)

		tab, err := eng.Tab(context.Background())
		if err != nil {
			t.Fatalf("case %d: Tab: %v", i, err)
		}
		err = eng.Reshape(context.Background(), tab, want)
		if err != nil && !errors.Is(err, tree.ErrNoChange) {
			t.Fatalf("case %d: Reshape(%s -> %s): %v\ncalls:\n%s", i, start, want, err, fake.callLog())
		}

		got := fake.treeOf(eng.TabID())
		if !tree.Equal(got, want) {
			t.Fatalf("case %d: %s -> %#v, want %#v\ncalls:\n%s", i, start, got, want, fake.callLog())
		}
		if tabs := fake.liveTabs(); len(tabs) != 1 {
			t.Fatalf("case %d: tabs = %v, want only the home tab\ncalls:\n%s", i, tabs, fake.callLog())
		}
		if fake.focused != eng.PaneID() {
			t.Fatalf("case %d: focus = %s, want the arranged pane %s", i, fake.focused, eng.PaneID())
		}
		assertNoJournal(t, stateDir)
	}
}

// TestEveryInterruptionKeepsEveryPane fails each call of a rebuild in turn and
// checks the invariant that matters most: a terminal is never destroyed, and it
// is either back in its tab or recorded for recovery.
func TestEveryInterruptionKeepsEveryPane(t *testing.T) {
	base := r(r(leaf("p1"), leaf("p2")), r(leaf("p3"), leaf("p4")))

	fake, eng, _ := setup(t, base.Clone(), "p2")
	if err := eng.ApplyPreset(context.Background(), tree.Tiled); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	total := fake.callCount

	for failAt := 1; failAt <= total; failAt++ {
		fake, eng, stateDir := setup(t, base.Clone(), "p2")
		fake.failAfter = failAt

		err := eng.ApplyPreset(context.Background(), tree.Tiled)
		if err == nil {
			continue // the failure landed on a call the plan did not make
		}

		if panes := fake.allPanes(); len(panes) != 4 {
			t.Fatalf("fail at %d: session holds %v, want four panes\ncalls:\n%s",
				failAt, panes, fake.callLog())
		}

		journal, jerr := loadJournal(stateDir)
		if jerr != nil {
			t.Fatalf("fail at %d: loadJournal: %v", failAt, jerr)
		}
		stranded := len(fake.liveTabs()) > 1
		recorded := journal != nil && len(journal.Panes) > 0
		if stranded && !recorded {
			t.Fatalf("fail at %d: panes sit in %v with no journal to recover them\ncalls:\n%s",
				failAt, fake.liveTabs(), fake.callLog())
		}
	}
}

func assertNoJournal(t *testing.T, stateDir string) {
	t.Helper()
	journal, err := loadJournal(stateDir)
	if err != nil {
		t.Fatalf("loadJournal: %v", err)
	}
	if journal != nil {
		t.Errorf("a parking journal was left behind: %+v", journal)
	}
}

func paneName(i int) string { return "p" + string(rune('1'+i)) }

func randomTree(g *rand.Rand, panes []string) *tree.Node {
	if len(panes) == 1 {
		return tree.Leaf(panes[0])
	}
	at := 1 + g.Intn(len(panes)-1)
	dir := herdr.Right
	if g.Intn(2) == 0 {
		dir = herdr.Down
	}
	ratio := tree.MinRatio + g.Float64()*(tree.MaxRatio-tree.MinRatio)
	return tree.Split(dir, ratio, randomTree(g, panes[:at]), randomTree(g, panes[at:]))
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
