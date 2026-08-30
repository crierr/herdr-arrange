package tree

import (
	"errors"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

// ErrNoChange means an operation would leave the tree exactly as it is. The UI
// turns this into a status-line flash rather than an error.
var ErrNoChange = errors.New("no change")

// ErrNotFound means the pane is not in the tree.
var ErrNotFound = errors.New("pane not in tab")

// Equalize returns the tree with every split re-weighted by leaf count, so every
// pane ends up with the same share of the tab. The shape is untouched, so
// applying this needs only layout.set_split_ratio calls: no pane moves, no
// flicker.
//
// Equal share means equal *area*, not equal width and height: [[a | b] | [c / d]]
// gives every pane a quarter of the tab while being four differently shaped
// rectangles, and Equalize leaves it alone because there is no ratio to change.
// Identical rectangles are a property of the shape, which is what the presets
// build — so the UI points at those when this has nothing to do.
//
// Ratios are clamped to what herdr accepts, so a hand-built chain deeper than
// ten panes cannot be made exactly even. EqualizeIsExact reports whether it
// worked.
func Equalize(n *Node) *Node {
	if n == nil || n.IsLeaf() {
		return n.Clone()
	}
	first, second := n.First.Count(), n.Second.Count()
	ratio := float64(first) / float64(first+second)
	return Split(n.Dir, Clamp(ratio), Equalize(n.First), Equalize(n.Second))
}

// EqualizeIsExact reports whether Equalize can give every pane an equal share,
// which it cannot when a split's true weight falls outside herdr's clamp.
func EqualizeIsExact(n *Node) bool {
	if n == nil || n.IsLeaf() {
		return true
	}
	first, second := n.First.Count(), n.Second.Count()
	ratio := float64(first) / float64(first+second)
	if Clamp(ratio) != ratio {
		return false
	}
	return EqualizeIsExact(n.First) && EqualizeIsExact(n.Second)
}

// ReSplit moves a pane to one side of a larger region of the tab: the shift-key
// counterpart of a swap.
//
// The pane detaches, its parent collapses onto the sibling subtree, and the pane
// re-attaches as a sibling of that whole subtree on the given side:
//
//	[A | [P / B]]  --left-->  [A | [P | B]]
//
// Pressing the same direction again walks the pane up another level, because the
// level-1 result would not change anything:
//
//	[A | [P | B]]  --left-->  [P | [A | B]]
//
// At the root the pane spans a full edge of the tab and further presses in that
// direction return ErrNoChange.
func ReSplit(n *Node, paneID string, dir herdr.Direction) (*Node, error) {
	if n == nil || !n.Has(paneID) {
		return nil, ErrNotFound
	}
	path, _ := n.Path(paneID)
	if len(path) == 0 {
		// The pane is the whole tab.
		return nil, ErrNoChange
	}

	splitDir, paneFirst := resolveDirection(dir)

	// Try the parent, then the grandparent, and so on: the first ancestor whose
	// rearrangement actually changes the tree wins.
	for level := 1; level <= len(path); level++ {
		ancestorPath := path[:len(path)-level]
		ancestor := at(n, ancestorPath)

		rest := remove(ancestor, paneID)
		if rest == nil {
			continue // cannot happen: an ancestor holds the pane plus a sibling
		}

		ratio := Clamp(reSplitRatio(rest.Count(), paneFirst))
		var replacement *Node
		if paneFirst {
			replacement = Split(splitDir, ratio, Leaf(paneID), rest)
		} else {
			replacement = Split(splitDir, ratio, rest, Leaf(paneID))
		}

		candidate := replaceAt(n, ancestorPath, replacement)
		if !SameShape(candidate, n) {
			return candidate, nil
		}
	}
	return nil, ErrNoChange
}

// resolveDirection maps a compass direction onto a split axis plus which side of
// that split the pane lands on.
func resolveDirection(dir herdr.Direction) (split herdr.SplitDirection, paneFirst bool) {
	switch dir {
	case herdr.Left:
		return herdr.Right, true
	case herdr.DirRt:
		return herdr.Right, false
	case herdr.Up:
		return herdr.Down, true
	default: // herdr.DirDn
		return herdr.Down, false
	}
}

// reSplitRatio gives the moved pane an even share against the region it now
// sits beside, so re-splitting next to five panes yields a sixth of the space
// rather than half.
func reSplitRatio(restLeaves int, paneFirst bool) float64 {
	share := 1.0 / float64(restLeaves+1)
	if paneFirst {
		return share
	}
	return 1 - share
}

// at returns the subtree reached by following a path of child choices.
func at(n *Node, path []bool) *Node {
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

// replaceAt returns a copy of n with the subtree at path replaced.
func replaceAt(n *Node, path []bool, sub *Node) *Node {
	if len(path) == 0 {
		return sub
	}
	if n == nil || n.IsLeaf() {
		return n.Clone()
	}
	first, second := n.First, n.Second
	if path[0] {
		second = replaceAt(second, path[1:], sub)
		first = first.Clone()
	} else {
		first = replaceAt(first, path[1:], sub)
		second = second.Clone()
	}
	return Split(n.Dir, n.Ratio, first, second)
}

// remove returns a copy of n without a pane, collapsing the split that held it
// onto its sibling. Returns nil if the pane was the whole tree.
func remove(n *Node, paneID string) *Node {
	if n == nil {
		return nil
	}
	if n.IsLeaf() {
		if n.PaneID == paneID {
			return nil
		}
		return Leaf(n.PaneID)
	}
	first, second := remove(n.First, paneID), remove(n.Second, paneID)
	switch {
	case first == nil:
		return second
	case second == nil:
		return first
	default:
		return Split(n.Dir, n.Ratio, first, second)
	}
}

// Insert returns a copy of n with pane added beside host, splitting host's slot
// along dir. host becomes the first child and pane the second, matching what
// herdr's pane.move does.
func Insert(n *Node, host, pane string, dir herdr.SplitDirection, ratio float64) *Node {
	path, ok := n.Path(host)
	if !ok {
		return n.Clone()
	}
	return replaceAt(n, path, Split(dir, ratio, Leaf(host), Leaf(pane)))
}

// Remove is the exported form of remove: the tree with one pane taken out.
func Remove(n *Node, paneID string) *Node { return remove(n, paneID) }

// SwapPanes returns a copy of n with two panes exchanged, which is what
// herdr's pane.swap does: positions and ratios stay, the panes trade places.
func SwapPanes(n *Node, a, b string) *Node {
	out := n.Clone()
	if a == b {
		return out
	}
	var rec func(*Node)
	rec = func(cur *Node) {
		if cur == nil {
			return
		}
		if cur.IsLeaf() {
			switch cur.PaneID {
			case a:
				cur.PaneID = b
			case b:
				cur.PaneID = a
			}
			return
		}
		rec(cur.First)
		rec(cur.Second)
	}
	rec(out)
	return out
}

// SetRatioAt returns a copy of n with one split's ratio changed, clamped the way
// herdr clamps it.
func SetRatioAt(n *Node, path []bool, ratio float64) *Node {
	target := at(n, path)
	if target == nil || target.IsLeaf() {
		return n.Clone()
	}
	return replaceAt(n, path, Split(target.Dir, Clamp(ratio), target.First, target.Second))
}
