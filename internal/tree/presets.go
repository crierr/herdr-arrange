package tree

import (
	"math"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

// Preset is one of the named layouts bound to keys 1-5.
//
// herdr has no built-in layout presets, so these are defined here.
type Preset int

const (
	EvenHorizontal Preset = iota
	EvenVertical
	MainHorizontal
	MainVertical
	Tiled
)

// Presets lists the presets in key order: 1-5.
var Presets = []Preset{EvenHorizontal, EvenVertical, MainHorizontal, MainVertical, Tiled}

// Name returns the preset's name as shown in the UI.
func (p Preset) Name() string {
	switch p {
	case EvenHorizontal:
		return "even-horizontal"
	case EvenVertical:
		return "even-vertical"
	case MainHorizontal:
		return "main-horizontal"
	case MainVertical:
		return "main-vertical"
	case Tiled:
		return "tiled"
	}
	return "custom"
}

// Build lays out panes according to the preset. main is the pane that becomes
// the large one in the main-* presets; it is ignored by the others. panes is
// the tab's panes in their current in-order, and must contain main.
//
// Returns nil for an empty pane list.
func (p Preset) Build(panes []string, main string) *Node {
	if len(panes) == 0 {
		return nil
	}
	if len(panes) == 1 {
		return Leaf(panes[0])
	}
	switch p {
	case EvenHorizontal:
		return balanced(panes, herdr.Right)
	case EvenVertical:
		return balanced(panes, herdr.Down)
	case MainHorizontal:
		return mainSplit(panes, main, herdr.Down, herdr.Right)
	case MainVertical:
		return mainSplit(panes, main, herdr.Right, herdr.Down)
	case Tiled:
		return tiled(panes)
	}
	return balanced(panes, herdr.Right)
}

// balanced builds an evenly-weighted tree over panes along one axis.
//
// A right-nested chain would render identically, but its ratios would have to
// be 1/n, 1/(n-1), ... — and herdr clamps every ratio to [0.1, 0.9], so a chain
// of more than ten panes could not be made even at all. Splitting down the
// middle keeps every ratio near 0.5 and the depth at O(log n).
func balanced(panes []string, dir herdr.SplitDirection) *Node {
	if len(panes) == 0 {
		return nil
	}
	if len(panes) == 1 {
		return Leaf(panes[0])
	}
	half := len(panes) / 2
	first, second := panes[:half], panes[half:]
	ratio := float64(len(first)) / float64(len(panes))
	return Split(dir, Clamp(ratio), balanced(first, dir), balanced(second, dir))
}

// mainSplit puts main in its own half along mainDir, with everything else
// stacked along restDir.
func mainSplit(panes []string, main string, mainDir, restDir herdr.SplitDirection) *Node {
	rest := make([]string, 0, len(panes))
	found := false
	for _, p := range panes {
		if p == main && !found {
			found = true
			continue
		}
		rest = append(rest, p)
	}
	if !found {
		// Caller passed a main that is not in the tab; fall back to the first
		// pane rather than producing a tree that drops a pane.
		main, rest = panes[0], panes[1:]
	}
	if len(rest) == 0 {
		return Leaf(main)
	}
	return Split(mainDir, 0.5, Leaf(main), balanced(rest, restDir))
}

// tiled builds a grid: ceil(sqrt(n)) columns of rows, rows stacked vertically.
func tiled(panes []string) *Node {
	n := len(panes)
	cols := max(int(math.Ceil(math.Sqrt(float64(n)))), 1)
	rows := make([]*Node, 0, (n+cols-1)/cols)
	weights := make([]int, 0, cap(rows))
	for i := 0; i < n; i += cols {
		end := min(i+cols, n)
		row := panes[i:end]
		rows = append(rows, balanced(row, herdr.Right))
		weights = append(weights, len(row))
	}
	return stack(rows, weights, herdr.Down)
}

// stack combines pre-built subtrees along one axis, weighting each split by the
// number of leaves on either side so every pane ends up the same size.
func stack(nodes []*Node, weights []int, dir herdr.SplitDirection) *Node {
	if len(nodes) == 0 {
		return nil
	}
	if len(nodes) == 1 {
		return nodes[0]
	}
	half := len(nodes) / 2
	firstWeight := sum(weights[:half])
	total := firstWeight + sum(weights[half:])
	ratio := float64(firstWeight) / float64(total)
	return Split(dir, Clamp(ratio),
		stack(nodes[:half], weights[:half], dir),
		stack(nodes[half:], weights[half:], dir))
}

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

// Detect reports which preset the tree matches, if any, taking its first leaf as
// the main pane — which is where Build puts one.
func Detect(n *Node) (Preset, bool) {
	if n == nil {
		return 0, false
	}
	return DetectFor(n, n.FirstLeaf())
}

// DetectFor reports which preset the tree matches with a given pane as the main
// one. It compares shape only, not ratios, so a preset the user has since
// resized is still recognised.
//
// The main pane matters: [A | [B / C]] is main-vertical for A but not for B, and
// naming it main-vertical while B is the pane being arranged would promise
// something `4` would then change. Detecting against the arranged pane keeps the
// status line, the number keys and `space` describing the same thing.
func DetectFor(n *Node, main string) (Preset, bool) {
	if n == nil {
		return 0, false
	}
	panes := n.Leaves()
	for _, p := range Presets {
		if SameShape(n, p.Build(panes, main)) {
			return p, true
		}
	}
	return 0, false
}

// Next returns the preset that `space` should apply: the first one after
// whatever the tree currently matches that would actually change something,
// with main as the pane the main-* presets should enlarge.
//
// Skipping no-ops matters because presets coincide at small pane counts — with
// two panes, a vertical stack is both even-vertical and main-horizontal — and a
// cycle that landed on a coincidence would appear to stall.
func Next(n *Node, main string) Preset {
	if n == nil {
		return Presets[0]
	}
	start := 0
	if cur, ok := DetectFor(n, main); ok {
		for i, p := range Presets {
			if p == cur {
				start = i + 1
				break
			}
		}
	}

	panes := n.Leaves()
	for offset := range len(Presets) {
		p := Presets[(start+offset)%len(Presets)]
		if !SameShape(n, p.Build(panes, main)) {
			return p
		}
	}
	// Every preset renders this tab identically (one pane, say). Return the next
	// one anyway so the key is never an error.
	return Presets[start%len(Presets)]
}
