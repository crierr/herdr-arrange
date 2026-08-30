package tree

import (
	"errors"
	"testing"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

func TestReSplitMovesPaneBesideItsParentsRemainder(t *testing.T) {
	tests := []struct {
		name string
		in   *Node
		pane string
		dir  herdr.Direction
		want string
	}{
		{
			name: "left, out of a vertical stack",
			in:   r(Leaf("a"), d(Leaf("p"), Leaf("b"))),
			pane: "p", dir: herdr.Left,
			want: "[a | [p | b]]",
		},
		{
			name: "right, out of a vertical stack",
			in:   r(Leaf("a"), d(Leaf("p"), Leaf("b"))),
			pane: "p", dir: herdr.DirRt,
			want: "[a | [b | p]]",
		},
		{
			name: "up, out of a horizontal row",
			in:   d(Leaf("a"), r(Leaf("p"), Leaf("b"))),
			pane: "p", dir: herdr.Up,
			want: "[a / [p / b]]",
		},
		{
			name: "down, out of a horizontal row",
			in:   d(Leaf("a"), r(Leaf("p"), Leaf("b"))),
			pane: "p", dir: herdr.DirDn,
			want: "[a / [b / p]]",
		},
		{
			name: "the remainder can be a whole subtree",
			in:   r(Leaf("a"), d(Leaf("p"), r(Leaf("b"), Leaf("c")))),
			pane: "p", dir: herdr.Left,
			want: "[a | [p | [b | c]]]",
		},
		{
			name: "already on that side, so it walks up a level",
			in:   r(Leaf("a"), r(Leaf("p"), Leaf("b"))),
			pane: "p", dir: herdr.Left,
			want: "[p | [a | b]]",
		},
		{
			name: "walking up from a deep nest",
			in:   r(Leaf("a"), d(Leaf("b"), r(Leaf("p"), Leaf("c")))),
			pane: "p", dir: herdr.Left,
			want: "[a | [p | [b / c]]]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReSplit(tc.in, tc.pane, tc.dir)
			if err != nil {
				t.Fatalf("ReSplit: %v", err)
			}
			if got.String() != tc.want {
				t.Errorf("ReSplit = %s, want %s", got, tc.want)
			}
			// No pane may be lost or duplicated.
			if got.Count() != tc.in.Count() {
				t.Errorf("Count = %d, want %d", got.Count(), tc.in.Count())
			}
		})
	}
}

func TestReSplitWalksUpOnRepeatedPresses(t *testing.T) {
	// Each press moves the pane one level further out, until it spans a full
	// edge of the tab and there is nowhere left to go.
	n := r(Leaf("a"), d(Leaf("p"), Leaf("b")))
	want := []string{
		"[a | [p | b]]",
		"[p | [a | b]]",
	}
	for i, expected := range want {
		next, err := ReSplit(n, "p", herdr.Left)
		if err != nil {
			t.Fatalf("press %d: %v", i+1, err)
		}
		if next.String() != expected {
			t.Fatalf("press %d = %s, want %s", i+1, next, expected)
		}
		n = next
	}
	if _, err := ReSplit(n, "p", herdr.Left); !errors.Is(err, ErrNoChange) {
		t.Errorf("at the tab edge, err = %v, want ErrNoChange", err)
	}
}

func TestReSplitRatioGivesTheMovedPaneAnEvenShare(t *testing.T) {
	// Moving a pane beside three others should give it a quarter, not a half.
	n := r(Leaf("p"), r(Leaf("a"), r(Leaf("b"), Leaf("c"))))
	got, err := ReSplit(n, "p", herdr.Up)
	if err != nil {
		t.Fatalf("ReSplit: %v", err)
	}
	if abs(got.Ratio-0.25) > 0.001 {
		t.Errorf("ratio = %.3f, want 0.25", got.Ratio)
	}

	// On the far side the pane is the `second` child, so it gets the complement.
	got, err = ReSplit(n, "p", herdr.DirDn)
	if err != nil {
		t.Fatalf("ReSplit: %v", err)
	}
	if abs(got.Ratio-0.75) > 0.001 {
		t.Errorf("ratio = %.3f, want 0.75", got.Ratio)
	}
}

func TestReSplitEdgeCases(t *testing.T) {
	if _, err := ReSplit(Leaf("p"), "p", herdr.Left); !errors.Is(err, ErrNoChange) {
		t.Errorf("single-pane tab: err = %v, want ErrNoChange", err)
	}
	if _, err := ReSplit(r(Leaf("a"), Leaf("b")), "zz", herdr.Left); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown pane: err = %v, want ErrNotFound", err)
	}
	if _, err := ReSplit(nil, "p", herdr.Left); !errors.Is(err, ErrNotFound) {
		t.Errorf("nil tree: err = %v, want ErrNotFound", err)
	}
}

func TestReSplitNeverLosesAPane(t *testing.T) {
	// Exhaustive over random trees: whatever direction is pressed on whatever
	// pane, the tab must still hold exactly the same panes.
	rng := newRand(7)
	for range 400 {
		before := randomTree(rng, 1+rng.intn(7))
		panes := before.Leaves()
		pane := panes[rng.intn(len(panes))]
		dir := []herdr.Direction{herdr.Left, herdr.DirRt, herdr.Up, herdr.DirDn}[rng.intn(4)]

		after, err := ReSplit(before, pane, dir)
		if err != nil {
			continue // ErrNoChange is a legitimate outcome
		}
		if !sameSet(before.Leaves(), after.Leaves()) {
			t.Fatalf("ReSplit(%s, %s, %s) = %s: pane set changed", before, pane, dir, after)
		}
		for _, ra := range after.Ratios() {
			if ra.Ratio < MinRatio || ra.Ratio > MaxRatio {
				t.Fatalf("ReSplit(%s, %s, %s) = %#v: ratio %v out of range", before, pane, dir, after, ra.Ratio)
			}
		}
	}
}

func TestEqualizeWeightsByLeafCount(t *testing.T) {
	// One pane on the left, three stacked on the right: the left pane should get
	// a quarter of the width.
	n := Split(herdr.Right, 0.8, Leaf("a"), Split(herdr.Down, 0.9, Leaf("b"), Split(herdr.Down, 0.2, Leaf("c"), Leaf("e"))))
	got := Equalize(n)

	if !SameShape(got, n) {
		t.Fatalf("Equalize changed the shape: %s", got)
	}
	if abs(got.Ratio-0.25) > 0.001 {
		t.Errorf("root ratio = %.3f, want 0.25", got.Ratio)
	}
	if abs(got.Second.Ratio-1.0/3.0) > 0.001 {
		t.Errorf("right stack ratio = %.3f, want 0.333", got.Second.Ratio)
	}
	if abs(got.Second.Second.Ratio-0.5) > 0.001 {
		t.Errorf("inner ratio = %.3f, want 0.5", got.Second.Second.Ratio)
	}
	if !EqualizeIsExact(n) {
		t.Error("this tree is equalizable exactly")
	}
}

func TestEqualizeIsInexactOutsideTheClamp(t *testing.T) {
	// A right-nested chain of twelve panes needs a 1/12 ratio at the root, which
	// herdr will clamp to 0.1. The UI says so rather than silently lying.
	chain := Leaf("p0")
	for i := 1; i < 12; i++ {
		chain = r(Leaf(paneName(i)), chain)
	}
	if EqualizeIsExact(chain) {
		t.Error("a 12-pane chain cannot be equalized within herdr's clamp")
	}
	got := Equalize(chain)
	if got.Ratio != MinRatio {
		t.Errorf("root ratio = %v, want the clamp %v", got.Ratio, MinRatio)
	}
	if !EqualizeIsExact(Leaf("only")) {
		t.Error("a single pane is trivially exact")
	}
}

func TestSwapPanesExchangesPositionsKeepingRatios(t *testing.T) {
	n := Split(herdr.Right, 0.3, Leaf("a"), Split(herdr.Down, 0.7, Leaf("b"), Leaf("c")))
	got := SwapPanes(n, "a", "c")
	if got.String() != "[c | [b / a]]" {
		t.Errorf("SwapPanes = %s, want [c | [b / a]]", got)
	}
	if got.Ratio != 0.3 || got.Second.Ratio != 0.7 {
		t.Error("SwapPanes should leave ratios alone")
	}
	if SwapPanes(n, "a", "a").String() != n.String() {
		t.Error("swapping a pane with itself is a no-op")
	}
}

func TestInsertMakesHostTheFirstChild(t *testing.T) {
	// herdr's split_at puts the existing pane in `first` and the arriving pane in
	// `second`; the reconciler depends on it.
	got := Insert(r(Leaf("a"), Leaf("b")), "b", "new", herdr.Down, 0.4)
	if got.String() != "[a | [b / new]]" {
		t.Errorf("Insert = %s, want [a | [b / new]]", got)
	}
	if abs(got.Second.Ratio-0.4) > 0.001 {
		t.Errorf("ratio = %v", got.Second.Ratio)
	}
}

func TestRemoveCollapsesTheParent(t *testing.T) {
	n := r(Leaf("a"), d(Leaf("b"), Leaf("c")))
	if got := Remove(n, "b"); got.String() != "[a | c]" {
		t.Errorf("Remove(b) = %s, want [a | c]", got)
	}
	if got := Remove(Leaf("only"), "only"); got != nil {
		t.Errorf("removing the last pane should give nil, got %s", got)
	}
}

func TestSetRatioAtClamps(t *testing.T) {
	n := r(Leaf("a"), Leaf("b"))
	if got := SetRatioAt(n, nil, 0.99); got.Ratio != MaxRatio {
		t.Errorf("ratio = %v, want %v", got.Ratio, MaxRatio)
	}
	// Addressing a leaf, or a path that runs off the tree, changes nothing.
	if got := SetRatioAt(n, []bool{false}, 0.3); got.String() != n.String() || got.Ratio != 0.5 {
		t.Errorf("SetRatioAt on a leaf should be a no-op, got %#v", got)
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[string]int{}
	for _, x := range a {
		count[x]++
	}
	for _, x := range b {
		count[x]--
		if count[x] < 0 {
			return false
		}
	}
	return true
}
