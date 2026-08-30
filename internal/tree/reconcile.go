package tree

import (
	"fmt"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

// StepKind is the sort of API call a plan step becomes.
type StepKind int

const (
	// StepSwap exchanges two panes within the tab (pane.swap). Cheap and
	// non-destructive, but cannot change the tab's shape.
	StepSwap StepKind = iota
	// StepSetRatio resizes one split (layout.set_split_ratio).
	StepSetRatio
	// StepPark moves a pane out to a scratch tab (pane.move to new_tab, or to
	// the scratch tab once it exists).
	StepPark
	// StepInsert moves a parked pane back beside Host (pane.move to tab).
	StepInsert
)

func (k StepKind) String() string {
	switch k {
	case StepSwap:
		return "swap"
	case StepSetRatio:
		return "set-ratio"
	case StepPark:
		return "park"
	case StepInsert:
		return "insert"
	}
	return "?"
}

// Step is one API call in a plan.
type Step struct {
	Kind StepKind

	// PaneID is the pane being swapped, parked or inserted.
	PaneID string
	// Target is the other pane for a swap, or the host to split for an insert.
	Target string
	// Split and Ratio apply to StepInsert.
	Split herdr.SplitDirection
	Ratio float64
	// Path and Ratio apply to StepSetRatio.
	Path []bool
}

func (s Step) String() string {
	switch s.Kind {
	case StepSwap:
		return fmt.Sprintf("swap %s <-> %s", s.PaneID, s.Target)
	case StepSetRatio:
		return fmt.Sprintf("set-ratio %v = %.2f", s.Path, s.Ratio)
	case StepPark:
		return fmt.Sprintf("park %s", s.PaneID)
	case StepInsert:
		return fmt.Sprintf("insert %s %s of %s @ %.2f", s.PaneID, s.Split, s.Target, s.Ratio)
	}
	return "?"
}

// Plan returns the steps that turn cur into want, choosing the cheapest correct
// strategy:
//
//   - identical trees need nothing;
//   - the same shape needs only ratio changes;
//   - the same frame with panes permuted needs only swaps (plus ratios);
//   - anything else needs a rebuild, because herdr has no in-place
//     restructure API (see Rebuild).
//
// cur and want must hold the same set of panes; Plan returns an error otherwise,
// since a plan that dropped or invented a pane would destroy a terminal.
func Plan(cur, want *Node) ([]Step, error) {
	if cur == nil || want == nil {
		return nil, fmt.Errorf("plan: nil tree")
	}
	if err := checkSamePanes(cur, want); err != nil {
		return nil, err
	}
	if Equal(cur, want) {
		return nil, nil
	}
	if SameShape(cur, want) {
		return ratioSteps(cur, want), nil
	}
	if SameFrame(cur, want) {
		steps := swapSteps(cur, want)
		// The swaps leave the frame untouched, so cur's ratios are still the ones
		// in play and can be compared against want's directly.
		return append(steps, ratioSteps(cur, want)...), nil
	}
	return Rebuild(cur, want)
}

// checkSamePanes verifies the two trees describe the same panes.
func checkSamePanes(cur, want *Node) error {
	inCur := map[string]int{}
	for _, p := range cur.Leaves() {
		inCur[p]++
	}
	inWant := map[string]int{}
	for _, p := range want.Leaves() {
		inWant[p]++
	}
	for p, n := range inWant {
		if inCur[p] != n {
			return fmt.Errorf("plan: target tree wants pane %s %d time(s), tab has %d", p, n, inCur[p])
		}
	}
	for p, n := range inCur {
		if inWant[p] != n {
			return fmt.Errorf("plan: target tree drops pane %s", p)
		}
	}
	return nil
}

// ratioSteps emits a set-ratio for each split whose ratio actually differs,
// comparing against the clamped target since that is all herdr will store.
func ratioSteps(cur, want *Node) []Step {
	curRatios, wantRatios := cur.Ratios(), want.Ratios()
	if len(curRatios) != len(wantRatios) {
		// Shapes differ; ask for every target ratio and let the server sort it out.
		curRatios = nil
	}
	var steps []Step
	for i, r := range wantRatios {
		target := Clamp(r.Ratio)
		if curRatios != nil && abs(curRatios[i].Ratio-target) <= ratioEpsilon {
			continue
		}
		steps = append(steps, Step{Kind: StepSetRatio, Path: r.Path, Ratio: target})
	}
	return steps
}

// swapSteps turns a permutation of panes into transpositions.
//
// Walking the leaf positions in order and swapping the wrong pane for the right
// one needs at most n-1 swaps, which is optimal to within one call per cycle.
func swapSteps(cur, want *Node) []Step {
	have := cur.Leaves()
	target := want.Leaves()

	// where[pane] is the position pane currently occupies.
	where := make(map[string]int, len(have))
	for i, p := range have {
		where[p] = i
	}

	var steps []Step
	for i, wantPane := range target {
		if have[i] == wantPane {
			continue
		}
		j := where[wantPane]
		a, b := have[i], have[j]
		steps = append(steps, Step{Kind: StepSwap, PaneID: a, Target: b})
		have[i], have[j] = b, a
		where[a], where[b] = j, i
	}
	return steps
}

// Rebuild produces the park-and-reinsert plan.
//
// herdr cannot restructure a tab in place: layout.apply respawns every pane
// (killing whatever is running in them) and pane.move refuses a move whose
// destination is the pane's own tab. But a move to *another* tab preserves the
// pane id, the terminal and the running process. So restructuring means moving
// panes out to a scratch tab and moving them back one at a time, each beside
// the pane it should sit next to.
//
// The in-order-first pane never moves. It anchors the tab, which both keeps the
// tab from being closed as empty and gives the first insert something to split.
func Rebuild(cur, want *Node) ([]Step, error) {
	if err := checkSamePanes(cur, want); err != nil {
		return nil, err
	}
	leaves := want.Leaves()
	if len(leaves) < 2 {
		return nil, nil
	}

	anchor := leaves[0]
	steps := make([]Step, 0, 3*len(leaves))

	// Park everything but the anchor. Order does not matter, so keep the tab's
	// current order: panes leave the visible tab in the order the user sees them.
	parked := map[string]bool{anchor: true}
	for _, p := range cur.Leaves() {
		if parked[p] {
			continue
		}
		parked[p] = true
		steps = append(steps, Step{Kind: StepPark, PaneID: p})
	}

	// Rebuild want from the anchor down. The invariant is that host is the
	// in-order-first leaf of node and already sits where node belongs, so the
	// second subtree's first leaf is what has to be moved in beside it.
	var materialize func(node *Node, host string) error
	materialize = func(node *Node, host string) error {
		if node.IsLeaf() {
			if node.PaneID != host {
				return fmt.Errorf("rebuild: expected %s at this slot, found %s", node.PaneID, host)
			}
			return nil
		}
		pane := node.Second.FirstLeaf()
		steps = append(steps, Step{
			Kind:   StepInsert,
			PaneID: pane,
			Target: host,
			Split:  node.Dir,
			Ratio:  Clamp(node.Ratio),
		})
		// host keeps the `first` slot of the split we just made; pane holds `second`.
		if err := materialize(node.First, host); err != nil {
			return err
		}
		return materialize(node.Second, pane)
	}
	if err := materialize(want, anchor); err != nil {
		return nil, err
	}

	// Each insert only approximates its ratio, because the slots it splits keep
	// being subdivided afterwards. Set them exactly at the end.
	for _, r := range want.Ratios() {
		steps = append(steps, Step{Kind: StepSetRatio, Path: r.Path, Ratio: Clamp(r.Ratio)})
	}
	return steps, nil
}

// Simulate applies a plan to a tree the way herdr would, and is the basis of the
// reconciler's tests: a plan is correct when simulating it reproduces the target.
//
// Parked panes are held aside and re-enter on their insert step.
func Simulate(cur *Node, steps []Step) (*Node, error) {
	out := cur.Clone()
	parked := map[string]bool{}

	for i, s := range steps {
		switch s.Kind {
		case StepSwap:
			if !out.Has(s.PaneID) || !out.Has(s.Target) {
				return nil, fmt.Errorf("step %d (%s): pane not in tab", i, s)
			}
			out = SwapPanes(out, s.PaneID, s.Target)

		case StepSetRatio:
			if at(out, s.Path) == nil {
				return nil, fmt.Errorf("step %d (%s): no split at path", i, s)
			}
			out = SetRatioAt(out, s.Path, s.Ratio)

		case StepPark:
			if !out.Has(s.PaneID) {
				return nil, fmt.Errorf("step %d (%s): pane not in tab", i, s)
			}
			out = Remove(out, s.PaneID)
			if out == nil {
				return nil, fmt.Errorf("step %d (%s): parked the last pane, tab would close", i, s)
			}
			parked[s.PaneID] = true

		case StepInsert:
			if !parked[s.PaneID] {
				return nil, fmt.Errorf("step %d (%s): pane is not parked", i, s)
			}
			if !out.Has(s.Target) {
				return nil, fmt.Errorf("step %d (%s): host not in tab", i, s)
			}
			out = Insert(out, s.Target, s.PaneID, s.Split, s.Ratio)
			delete(parked, s.PaneID)
		}
	}

	if len(parked) != 0 {
		return nil, fmt.Errorf("plan leaves %d pane(s) parked", len(parked))
	}
	return out, nil
}
