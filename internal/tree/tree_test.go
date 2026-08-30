package tree

import (
	"testing"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

// r builds a right split, d a down split, at an even ratio. Tests that care
// about ratios set them explicitly.
func r(first, second *Node) *Node { return Split(herdr.Right, 0.5, first, second) }
func d(first, second *Node) *Node { return Split(herdr.Down, 0.5, first, second) }

func TestFromLayoutConvertsExport(t *testing.T) {
	export := &herdr.LayoutNode{
		Type: "split", Direction: herdr.Right, Ratio: 0.3,
		First: &herdr.LayoutNode{Type: "pane", PaneID: "p1"},
		Second: &herdr.LayoutNode{
			Type: "split", Direction: herdr.Down, Ratio: 0.7,
			First:  &herdr.LayoutNode{Type: "pane", PaneID: "p2"},
			Second: &herdr.LayoutNode{Type: "pane", PaneID: "p3"},
		},
	}
	got := FromLayout(export)
	if got.String() != "[p1 | [p2 / p3]]" {
		t.Fatalf("String = %s", got)
	}
	if got.Ratio != 0.3 || got.Second.Ratio != 0.7 {
		t.Errorf("ratios = %.2f, %.2f", got.Ratio, got.Second.Ratio)
	}
}

func TestFromLayoutSkipsPanelessLeaves(t *testing.T) {
	// A pane node with no id cannot be addressed, so it collapses away rather
	// than becoming a leaf the planner would try to move.
	export := &herdr.LayoutNode{
		Type: "split", Direction: herdr.Right, Ratio: 0.5,
		First:  &herdr.LayoutNode{Type: "pane", PaneID: "p1"},
		Second: &herdr.LayoutNode{Type: "pane"},
	}
	got := FromLayout(export)
	if got.String() != "p1" {
		t.Errorf("String = %s, want p1", got)
	}
	if FromLayout(nil) != nil {
		t.Error("FromLayout(nil) should be nil")
	}
}

func TestLeavesAreInOrder(t *testing.T) {
	n := r(Leaf("a"), d(Leaf("b"), r(Leaf("c"), Leaf("e"))))
	got := n.Leaves()
	want := []string{"a", "b", "c", "e"}
	if len(got) != len(want) {
		t.Fatalf("Leaves = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Leaves = %v, want %v", got, want)
		}
	}
	if n.Count() != 4 {
		t.Errorf("Count = %d", n.Count())
	}
	if n.FirstLeaf() != "a" {
		t.Errorf("FirstLeaf = %s", n.FirstLeaf())
	}
}

func TestPath(t *testing.T) {
	n := r(Leaf("a"), d(Leaf("b"), Leaf("c")))
	for _, tc := range []struct {
		pane string
		want []bool
		ok   bool
	}{
		{"a", []bool{false}, true},
		{"b", []bool{true, false}, true},
		{"c", []bool{true, true}, true},
		{"zz", nil, false},
	} {
		got, ok := n.Path(tc.pane)
		if ok != tc.ok {
			t.Errorf("Path(%s) ok = %v, want %v", tc.pane, ok, tc.ok)
			continue
		}
		if !pathEqual(got, tc.want) {
			t.Errorf("Path(%s) = %v, want %v", tc.pane, got, tc.want)
		}
	}
	if n.Has("zz") {
		t.Error("Has(zz) should be false")
	}
}

func TestPathsDoNotAliasBetweenSiblings(t *testing.T) {
	// Sibling recursions must not share path storage, or deep trees report the
	// wrong split addresses and set_split_ratio resizes the wrong split.
	n := r(d(Leaf("a"), Leaf("b")), d(Leaf("c"), Leaf("e")))
	paths := map[string][]bool{}
	for _, p := range n.Leaves() {
		got, _ := n.Path(p)
		paths[p] = got
	}
	want := map[string][]bool{
		"a": {false, false}, "b": {false, true},
		"c": {true, false}, "e": {true, true},
	}
	for pane, wantPath := range want {
		if !pathEqual(paths[pane], wantPath) {
			t.Errorf("Path(%s) = %v, want %v", pane, paths[pane], wantPath)
		}
	}
}

func TestRatiosArePreOrderWithPaths(t *testing.T) {
	n := Split(herdr.Right, 0.3, Leaf("a"), Split(herdr.Down, 0.7, Leaf("b"), Leaf("c")))
	got := n.Ratios()
	if len(got) != 2 {
		t.Fatalf("Ratios = %v", got)
	}
	if len(got[0].Path) != 0 || got[0].Ratio != 0.3 {
		t.Errorf("root ratio = %+v", got[0])
	}
	if !pathEqual(got[1].Path, []bool{true}) || got[1].Ratio != 0.7 {
		t.Errorf("nested ratio = %+v", got[1])
	}
	if len(Leaf("a").Ratios()) != 0 {
		t.Error("a leaf has no ratios")
	}
}

func TestSameShapeAndSameFrame(t *testing.T) {
	base := r(Leaf("a"), d(Leaf("b"), Leaf("c")))

	sameShapeDifferentRatio := Split(herdr.Right, 0.2, Leaf("a"), Split(herdr.Down, 0.8, Leaf("b"), Leaf("c")))
	permuted := r(Leaf("c"), d(Leaf("a"), Leaf("b")))
	differentDir := d(Leaf("a"), d(Leaf("b"), Leaf("c")))
	differentStructure := r(r(Leaf("a"), Leaf("b")), Leaf("c"))

	if !SameShape(base, sameShapeDifferentRatio) {
		t.Error("ratios should not affect SameShape")
	}
	if SameShape(base, permuted) {
		t.Error("a permutation is not the same shape")
	}
	if !SameFrame(base, permuted) {
		t.Error("a permutation has the same frame")
	}
	if SameFrame(base, differentDir) || SameFrame(base, differentStructure) {
		t.Error("different structure should not match a frame")
	}
	if !SameShape(nil, nil) || SameShape(base, nil) {
		t.Error("nil handling in SameShape")
	}
}

func TestEqualToleratesTinyRatioDrift(t *testing.T) {
	a := Split(herdr.Right, 0.5, Leaf("x"), Leaf("y"))
	b := Split(herdr.Right, 0.5005, Leaf("x"), Leaf("y"))
	c := Split(herdr.Right, 0.6, Leaf("x"), Leaf("y"))
	if !Equal(a, b) {
		t.Error("sub-epsilon drift should compare equal")
	}
	if Equal(a, c) {
		t.Error("0.1 apart should not compare equal")
	}
}

func TestClamp(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{0.5, 0.5}, {0.0, MinRatio}, {-1, MinRatio}, {1.0, MaxRatio}, {0.05, MinRatio}, {0.95, MaxRatio},
	} {
		if got := Clamp(tc.in); got != tc.want {
			t.Errorf("Clamp(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCloneIsDeep(t *testing.T) {
	orig := r(Leaf("a"), Leaf("b"))
	cp := orig.Clone()
	cp.First.PaneID = "mutated"
	cp.Ratio = 0.9
	if orig.First.PaneID != "a" || orig.Ratio != 0.5 {
		t.Error("Clone shares state with the original")
	}
}

func pathEqual(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
