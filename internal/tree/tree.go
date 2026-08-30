// Package tree models herdr's binary split tree and computes the API calls
// needed to turn one tree into another.
//
// Everything here is a pure function over immutable trees: no I/O, no sockets.
// The executor in package exec is what actually talks to herdr.
package tree

import (
	"fmt"
	"strings"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

// herdr clamps every split ratio to this range (Layout::set_ratio_at), so a
// tree we ask for is only reachable if its ratios already lie inside it.
const (
	MinRatio = 0.1
	MaxRatio = 0.9
)

// Clamp restricts a ratio to the range herdr accepts.
func Clamp(r float64) float64 {
	switch {
	case r < MinRatio:
		return MinRatio
	case r > MaxRatio:
		return MaxRatio
	default:
		return r
	}
}

// Node is a split tree: either a leaf holding one pane, or a split with two
// children. A nil *Node is not a valid tree.
type Node struct {
	// PaneID is set on leaves only.
	PaneID string

	// Split fields, set when First and Second are non-nil.
	Dir    herdr.SplitDirection
	Ratio  float64
	First  *Node
	Second *Node
}

// Leaf returns a leaf node for a pane.
func Leaf(paneID string) *Node { return &Node{PaneID: paneID} }

// Split returns a split node. ratio is the fraction given to first.
func Split(dir herdr.SplitDirection, ratio float64, first, second *Node) *Node {
	return &Node{Dir: dir, Ratio: ratio, First: first, Second: second}
}

// IsLeaf reports whether n holds a single pane.
func (n *Node) IsLeaf() bool { return n != nil && n.First == nil }

// FromLayout converts a layout.export tree. Leaves without a pane id (herdr
// allows them in layout *specs*, never in exports of a live tab) are dropped,
// collapsing their parent. Returns nil if nothing is left.
func FromLayout(n *herdr.LayoutNode) *Node {
	if n == nil {
		return nil
	}
	if n.IsPane() {
		if n.PaneID == "" {
			return nil
		}
		return Leaf(n.PaneID)
	}
	first, second := FromLayout(n.First), FromLayout(n.Second)
	switch {
	case first == nil:
		return second
	case second == nil:
		return first
	default:
		return Split(n.Direction, n.Ratio, first, second)
	}
}

// Clone returns a deep copy.
func (n *Node) Clone() *Node {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		return Leaf(n.PaneID)
	}
	return Split(n.Dir, n.Ratio, n.First.Clone(), n.Second.Clone())
}

// Leaves returns the pane ids in in-order: left-to-right for a "right" split,
// top-to-bottom for a "down" split. This is the order herdr's own layout export
// uses, so it is the canonical reading order of a tab.
func (n *Node) Leaves() []string {
	var out []string
	n.walk(func(leaf *Node, _ []bool) { out = append(out, leaf.PaneID) })
	return out
}

// Count returns the number of leaves.
func (n *Node) Count() int {
	if n == nil {
		return 0
	}
	if n.IsLeaf() {
		return 1
	}
	return n.First.Count() + n.Second.Count()
}

// FirstLeaf returns the in-order first pane id, or "" for a nil tree.
func (n *Node) FirstLeaf() string {
	for n != nil && !n.IsLeaf() {
		n = n.First
	}
	if n == nil {
		return ""
	}
	return n.PaneID
}

// childPath extends a path by one choice, always in fresh storage so sibling
// recursions can never alias each other's paths.
func childPath(path []bool, choice bool) []bool {
	out := make([]bool, len(path)+1)
	copy(out, path)
	out[len(path)] = choice
	return out
}

// walk visits every leaf in order, passing the path of child choices taken to
// reach it (false = first, true = second).
func (n *Node) walk(fn func(leaf *Node, path []bool)) {
	if n == nil {
		return
	}
	var rec func(*Node, []bool)
	rec = func(cur *Node, path []bool) {
		if cur.IsLeaf() {
			fn(cur, path)
			return
		}
		rec(cur.First, childPath(path, false))
		rec(cur.Second, childPath(path, true))
	}
	rec(n, nil)
}

// Path returns the child choices leading to a pane's leaf, and whether it was
// found. The root itself has the empty path.
func (n *Node) Path(paneID string) ([]bool, bool) {
	var found []bool
	ok := false
	n.walk(func(leaf *Node, path []bool) {
		if !ok && leaf.PaneID == paneID {
			found = append([]bool(nil), path...)
			ok = true
		}
	})
	return found, ok
}

// Has reports whether the tree contains a pane.
func (n *Node) Has(paneID string) bool {
	_, ok := n.Path(paneID)
	return ok
}

// RatioAt is one split's ratio, addressed the way layout.set_split_ratio wants.
type RatioAt struct {
	Path  []bool
	Ratio float64
}

// Ratios returns every split's ratio in pre-order.
func (n *Node) Ratios() []RatioAt {
	var out []RatioAt
	var rec func(*Node, []bool)
	rec = func(cur *Node, path []bool) {
		if cur == nil || cur.IsLeaf() {
			return
		}
		out = append(out, RatioAt{Path: path, Ratio: cur.Ratio})
		rec(cur.First, childPath(path, false))
		rec(cur.Second, childPath(path, true))
	}
	rec(n, nil)
	return out
}

// SameShape reports whether two trees have identical structure, split
// directions and pane placement, ignoring ratios.
func SameShape(a, b *Node) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	case a.IsLeaf() != b.IsLeaf():
		return false
	case a.IsLeaf():
		return a.PaneID == b.PaneID
	case a.Dir != b.Dir:
		return false
	default:
		return SameShape(a.First, b.First) && SameShape(a.Second, b.Second)
	}
}

// SameFrame reports whether two trees have identical structure and split
// directions, ignoring which pane sits in which leaf and ignoring ratios.
// Two trees with the same frame differ only by a permutation of their panes,
// which pane.swap can fix without restructuring.
func SameFrame(a, b *Node) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	case a.IsLeaf() != b.IsLeaf():
		return false
	case a.IsLeaf():
		return true
	case a.Dir != b.Dir:
		return false
	default:
		return SameFrame(a.First, b.First) && SameFrame(a.Second, b.Second)
	}
}

// ratioEpsilon is the tolerance for calling two ratios equal. herdr stores
// ratios as f32 and rounds them to cell boundaries when rendering, so
// differences well below one percent are not worth an API call.
const ratioEpsilon = 0.005

// Equal reports whether two trees are the same shape with ratios equal to
// within ratioEpsilon.
func Equal(a, b *Node) bool {
	switch {
	case a == nil || b == nil:
		return a == b
	case a.IsLeaf() != b.IsLeaf():
		return false
	case a.IsLeaf():
		return a.PaneID == b.PaneID
	case a.Dir != b.Dir:
		return false
	case abs(a.Ratio-b.Ratio) > ratioEpsilon:
		return false
	default:
		return Equal(a.First, b.First) && Equal(a.Second, b.Second)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// String renders the tree compactly for tests and debugging: "|" for a right
// split, "/" for a down split.
func (n *Node) String() string {
	if n == nil {
		return "<nil>"
	}
	if n.IsLeaf() {
		return n.PaneID
	}
	sep := "|"
	if n.Dir == herdr.Down {
		sep = "/"
	}
	return fmt.Sprintf("[%s %s %s]", n.First, sep, n.Second)
}

// GoString renders the tree with ratios, for test failure messages.
func (n *Node) GoString() string {
	if n == nil {
		return "<nil>"
	}
	if n.IsLeaf() {
		return n.PaneID
	}
	sep := "|"
	if n.Dir == herdr.Down {
		sep = "/"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[%#v %s%.2f %#v]", n.First, sep, n.Ratio, n.Second)
	return b.String()
}
