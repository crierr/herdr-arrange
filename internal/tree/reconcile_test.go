package tree

import (
	"strings"
	"testing"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

func TestPlanDoesNothingWhenAlreadyThere(t *testing.T) {
	n := r(Leaf("a"), d(Leaf("b"), Leaf("c")))
	steps, err := Plan(n, n.Clone())
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("steps = %v, want none", steps)
	}
}

func TestPlanUsesOnlyRatiosWhenTheShapeMatches(t *testing.T) {
	cur := Split(herdr.Right, 0.5, Leaf("a"), Split(herdr.Down, 0.5, Leaf("b"), Leaf("c")))
	want := Split(herdr.Right, 0.3, Leaf("a"), Split(herdr.Down, 0.5, Leaf("b"), Leaf("c")))

	steps, err := Plan(cur, want)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	// Only the root ratio changed, so only the root should be touched: this is
	// what makes `e` (equalize) free of flicker.
	if len(steps) != 1 {
		t.Fatalf("steps = %v, want 1", steps)
	}
	if steps[0].Kind != StepSetRatio || len(steps[0].Path) != 0 || abs(steps[0].Ratio-0.3) > 0.001 {
		t.Errorf("step = %s", steps[0])
	}
	assertPlanReaches(t, cur, want, steps)
}

func TestPlanUsesSwapsForAPermutation(t *testing.T) {
	cur := r(Leaf("a"), d(Leaf("b"), Leaf("c")))
	want := r(Leaf("c"), d(Leaf("a"), Leaf("b")))

	steps, err := Plan(cur, want)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	for _, s := range steps {
		if s.Kind == StepPark || s.Kind == StepInsert {
			t.Fatalf("a permutation should never move a pane between tabs: %v", steps)
		}
	}
	// A 3-cycle takes two transpositions.
	if n := countKind(steps, StepSwap); n != 2 {
		t.Errorf("swaps = %d, want 2 (%v)", n, steps)
	}
	assertPlanReaches(t, cur, want, steps)
}

func TestPlanRebuildsWhenTheShapeChanges(t *testing.T) {
	cur := r(Leaf("a"), r(Leaf("b"), Leaf("c")))
	want := r(r(Leaf("a"), Leaf("b")), Leaf("c"))

	steps, err := Plan(cur, want)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if countKind(steps, StepPark) == 0 {
		t.Fatalf("a reshape needs a rebuild: %v", steps)
	}
	assertPlanReaches(t, cur, want, steps)
}

func TestPlanRejectsAPlanThatWouldLoseAPane(t *testing.T) {
	cur := r(Leaf("a"), Leaf("b"))

	for _, tc := range []struct {
		name string
		want *Node
		msg  string
	}{
		{"drops a pane", Leaf("a"), "drops pane b"},
		{"invents a pane", r(Leaf("a"), r(Leaf("b"), Leaf("ghost"))), "wants pane ghost"},
		{"duplicates a pane", r(Leaf("a"), r(Leaf("b"), Leaf("b"))), "wants pane b 2 time(s)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Plan(cur, tc.want)
			if err == nil {
				t.Fatal("want an error: a bad plan would destroy a terminal")
			}
			if !strings.Contains(err.Error(), tc.msg) {
				t.Errorf("err = %v, want it to mention %q", err, tc.msg)
			}
		})
	}
}

func TestRebuildKeepsTheAnchorInPlace(t *testing.T) {
	// The in-order-first pane of the target must never be parked, or the tab
	// could be closed for being empty mid-rebuild.
	cur := r(Leaf("a"), r(Leaf("b"), Leaf("c")))
	want := d(Leaf("c"), r(Leaf("a"), Leaf("b")))

	steps, err := Rebuild(cur, want)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	for _, s := range steps {
		if s.Kind == StepPark && s.PaneID == "c" {
			t.Errorf("parked the anchor: %v", steps)
		}
	}
	if n := countKind(steps, StepPark); n != 2 {
		t.Errorf("parks = %d, want 2 (every pane but the anchor)", n)
	}
	assertPlanReaches(t, cur, want, steps)
}

func TestRebuildIsANoOpBelowTwoPanes(t *testing.T) {
	steps, err := Rebuild(Leaf("a"), Leaf("a"))
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("steps = %v, want none", steps)
	}
}

func TestRebuildInsertsEveryParkedPaneExactlyOnce(t *testing.T) {
	cur := r(Leaf("a"), r(Leaf("b"), r(Leaf("c"), Leaf("e"))))
	want := d(r(Leaf("e"), Leaf("c")), r(Leaf("b"), Leaf("a")))

	steps, err := Rebuild(cur, want)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	parked, inserted := map[string]int{}, map[string]int{}
	for _, s := range steps {
		switch s.Kind {
		case StepPark:
			parked[s.PaneID]++
		case StepInsert:
			inserted[s.PaneID]++
		}
	}
	for pane, n := range parked {
		if n != 1 {
			t.Errorf("pane %s parked %d times", pane, n)
		}
		if inserted[pane] != 1 {
			t.Errorf("pane %s parked but inserted %d times", pane, inserted[pane])
		}
	}
	if len(inserted) != len(parked) {
		t.Errorf("inserted %d panes but parked %d", len(inserted), len(parked))
	}
	assertPlanReaches(t, cur, want, steps)
}

// TestPlanReachesAnyTargetTree is the reconciler's main guarantee: for any pair
// of trees over the same panes, executing Plan's steps produces the target.
func TestPlanReachesAnyTargetTree(t *testing.T) {
	g := newRand(1)
	for i := range 3000 {
		n := 1 + g.intn(12)
		cur := randomTree(g, n)
		panes := make([]string, n)
		for j := range panes {
			panes[j] = paneName(j)
		}
		want := randomTreeOver(g, g.shuffled(panes))

		steps, err := Plan(cur, want)
		if err != nil {
			t.Fatalf("case %d: Plan(%s -> %s): %v", i, cur, want, err)
		}
		got, err := Simulate(cur, steps)
		if err != nil {
			t.Fatalf("case %d: Simulate(%s -> %s): %v\nsteps: %v", i, cur, want, err, steps)
		}
		if !Equal(got, want) {
			t.Fatalf("case %d: Plan(%s -> %s) produced %#v, want %#v\nsteps: %v",
				i, cur, want, got, want, steps)
		}
	}
}

// TestPlanReachesEveryPreset covers the 1-5 keys at every pane count the presets
// are interesting at, from every starting shape.
func TestPlanReachesEveryPreset(t *testing.T) {
	g := newRand(2)
	for n := 1; n <= 9; n++ {
		panes := make([]string, n)
		for i := range panes {
			panes[i] = paneName(i)
		}
		for _, preset := range Presets {
			for _, main := range panes {
				for range 3 {
					cur := randomTreeOver(g, g.shuffled(panes))
					want := preset.Build(cur.Leaves(), main)

					steps, err := Plan(cur, want)
					if err != nil {
						t.Fatalf("%s n=%d main=%s: Plan(%s): %v", preset.Name(), n, main, cur, err)
					}
					got, err := Simulate(cur, steps)
					if err != nil {
						t.Fatalf("%s n=%d main=%s: Simulate(%s): %v", preset.Name(), n, main, cur, err)
					}
					if !Equal(got, want) {
						t.Fatalf("%s n=%d main=%s from %s: got %#v, want %#v",
							preset.Name(), n, main, cur, got, want)
					}
				}
			}
		}
	}
}

// TestPlanReachesEveryReSplit covers the H/J/K/L keys.
func TestPlanReachesEveryReSplit(t *testing.T) {
	g := newRand(3)
	dirs := []herdr.Direction{herdr.Left, herdr.DirRt, herdr.Up, herdr.DirDn}
	for range 2000 {
		cur := randomTree(g, 1+g.intn(8))
		panes := cur.Leaves()
		pane := panes[g.intn(len(panes))]
		dir := dirs[g.intn(len(dirs))]

		want, err := ReSplit(cur, pane, dir)
		if err != nil {
			continue
		}
		steps, err := Plan(cur, want)
		if err != nil {
			t.Fatalf("Plan(%s, resplit %s %s): %v", cur, pane, dir, err)
		}
		got, err := Simulate(cur, steps)
		if err != nil {
			t.Fatalf("Simulate(%s, resplit %s %s): %v\nsteps: %v", cur, pane, dir, err, steps)
		}
		if !Equal(got, want) {
			t.Fatalf("resplit %s %s from %s: got %#v, want %#v\nsteps: %v",
				pane, dir, cur, got, want, steps)
		}
	}
}

// TestPlanReachesBalance covers the `e` key, and checks it never moves a pane:
// balancing is ratios only, whatever shape the tab is in.
func TestPlanReachesBalance(t *testing.T) {
	g := newRand(4)
	for range 500 {
		cur := randomTree(g, 1+g.intn(9))
		want := Balance(cur)

		steps, err := Plan(cur, want)
		if err != nil {
			t.Fatalf("Plan(%s, balance): %v", cur, err)
		}
		for _, s := range steps {
			if s.Kind != StepSetRatio {
				t.Fatalf("balancing should only resize, got %s in %v", s, steps)
			}
		}
		got, err := Simulate(cur, steps)
		if err != nil {
			t.Fatalf("Simulate: %v", err)
		}
		if !Equal(got, want) {
			t.Fatalf("balance %s: got %#v, want %#v", cur, got, want)
		}
		// Balancing an already balanced tab is a no-op, so `e` settles.
		if !Equal(Balance(got), got) {
			t.Fatalf("balance %s is not settled: %#v", cur, Balance(got))
		}
	}
}

func TestSimulateRejectsIncoherentPlans(t *testing.T) {
	base := r(Leaf("a"), Leaf("b"))
	for _, tc := range []struct {
		name  string
		steps []Step
		msg   string
	}{
		{
			name:  "inserting a pane that was never parked",
			steps: []Step{{Kind: StepInsert, PaneID: "ghost", Target: "a", Split: herdr.Right, Ratio: 0.5}},
			msg:   "not parked",
		},
		{
			name:  "swapping a pane that is not in the tab",
			steps: []Step{{Kind: StepSwap, PaneID: "a", Target: "ghost"}},
			msg:   "pane not in tab",
		},
		{
			name:  "leaving a pane parked",
			steps: []Step{{Kind: StepPark, PaneID: "b"}},
			msg:   "leaves 1 pane(s) parked",
		},
		{
			name:  "parking the last pane",
			steps: []Step{{Kind: StepPark, PaneID: "a"}, {Kind: StepPark, PaneID: "b"}},
			msg:   "tab would close",
		},
		{
			name:  "resizing a split that does not exist",
			steps: []Step{{Kind: StepSetRatio, Path: []bool{true, true, false}, Ratio: 0.5}},
			msg:   "no split at path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Simulate(base, tc.steps)
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), tc.msg) {
				t.Errorf("err = %v, want it to mention %q", err, tc.msg)
			}
		})
	}
}

func TestPlanRatiosStayInsideTheClamp(t *testing.T) {
	// Asking for a ratio herdr will not store would leave the plan permanently
	// "unfinished", so Plan must only ever ask for reachable ratios.
	g := newRand(5)
	for range 500 {
		cur := randomTree(g, 2+g.intn(8))
		want := randomTreeOver(g, g.shuffled(cur.Leaves()))
		// Deliberately push some ratios out of range.
		want.Ratio = 0.99

		steps, err := Plan(cur, want)
		if err != nil {
			t.Fatalf("Plan: %v", err)
		}
		for _, s := range steps {
			if s.Kind != StepSetRatio && s.Kind != StepInsert {
				continue
			}
			if s.Ratio < MinRatio || s.Ratio > MaxRatio {
				t.Fatalf("step %s asks for an unreachable ratio", s)
			}
		}
	}
}

func countKind(steps []Step, kind StepKind) int {
	n := 0
	for _, s := range steps {
		if s.Kind == kind {
			n++
		}
	}
	return n
}

// assertPlanReaches checks that executing steps on cur produces want.
func assertPlanReaches(t *testing.T, cur, want *Node, steps []Step) {
	t.Helper()
	got, err := Simulate(cur, steps)
	if err != nil {
		t.Fatalf("Simulate: %v\nsteps: %v", err, steps)
	}
	if !Equal(got, want) {
		t.Fatalf("plan produced %#v, want %#v\nsteps: %v", got, want, steps)
	}
}
