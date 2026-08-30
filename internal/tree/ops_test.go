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

// shares returns each pane's fraction of the tab along dir. A split the other
// way does not divide anything along dir, so both its sides get the whole
// fraction: what comes out is how wide (or tall) each pane's column is.
func shares(n *Node, dir herdr.SplitDirection) map[string]float64 {
	out := map[string]float64{}
	var rec func(*Node, float64)
	rec = func(cur *Node, frac float64) {
		if cur.IsLeaf() {
			out[cur.PaneID] = frac
			return
		}
		if cur.Dir != dir {
			rec(cur.First, frac)
			rec(cur.Second, frac)
			return
		}
		rec(cur.First, frac*cur.Ratio)
		rec(cur.Second, frac*(1-cur.Ratio))
	}
	rec(n, 1)
	return out
}

// assertShares checks each pane's room along dir against want, keyed by pane.
func assertShares(t *testing.T, n *Node, dir herdr.SplitDirection, want map[string]float64) {
	t.Helper()
	got := shares(n, dir)
	for pane, w := range want {
		if abs(got[pane]-w) > 0.001 {
			t.Errorf("%s: pane %s has %.3f of the tab along %s, want %.3f", n, pane, got[pane], dir, w)
		}
	}
}

func TestBalanceEvensOutARunOfCells(t *testing.T) {
	// Four columns, badly weighted: every pane should end up a quarter wide, and
	// the shape should not move at all.
	n := Split(herdr.Right, 0.8,
		Leaf("a"),
		Split(herdr.Right, 0.9, Leaf("b"), Split(herdr.Right, 0.2, Leaf("c"), Leaf("d"))))

	got := Balance(n)
	if !SameShape(got, n) {
		t.Fatalf("Balance changed the shape: %s", got)
	}
	assertShares(t, got, herdr.Right, map[string]float64{"a": 0.25, "b": 0.25, "c": 0.25, "d": 0.25})
	if abs(got.Ratio-0.25) > 0.001 || abs(got.Second.Ratio-1.0/3.0) > 0.001 {
		t.Errorf("ratios = %#v, want 0.25 then 0.333", got)
	}
	if !BalanceIsExact(n) {
		t.Error("four columns balance exactly")
	}
}

func TestBalanceCountsASplitTheOtherWayAsOneCell(t *testing.T) {
	// [[a | b] | [c / d]] is three columns, the last one split in two. Balancing
	// gives the three columns a third of the width each — it does not flatten the
	// stack, and it does not chase equal areas.
	n := Split(herdr.Right, 0.5,
		Split(herdr.Right, 0.5, Leaf("a"), Leaf("b")),
		Split(herdr.Down, 0.5, Leaf("c"), Leaf("d")))

	got := Balance(n)
	if !SameShape(got, n) {
		t.Fatalf("Balance changed the shape: %s", got)
	}
	if abs(got.Ratio-2.0/3.0) > 0.001 {
		t.Errorf("root ratio = %.3f, want 0.667", got.Ratio)
	}
	assertShares(t, got, herdr.Right, map[string]float64{"a": 1.0 / 3, "b": 1.0 / 3, "c": 1.0 / 3, "d": 1.0 / 3})
	// c and d share that column, so they keep half its height each.
	assertShares(t, got, herdr.Down, map[string]float64{"a": 1, "b": 1, "c": 0.5, "d": 0.5})
}

func TestBalanceLeavesTheMainPresetsAlone(t *testing.T) {
	// The main-* presets are a main pane against everything else: one cell each
	// way, so balancing keeps the main pane's half rather than shrinking it to
	// 1/n. That is what makes `e` safe to press on any layout.
	for _, preset := range []Preset{MainHorizontal, MainVertical, EvenHorizontal, EvenVertical} {
		for n := 1; n <= 9; n++ {
			built := preset.Build(panes(n), "p0")
			if got := Balance(built); !Equal(got, built) {
				t.Errorf("%s n=%d: Balance changed it to %#v", preset.Name(), n, got)
			}
		}
	}
}

func TestBalanceIsInexactOutsideTheClamp(t *testing.T) {
	// A chain of twelve columns needs a 1/12 ratio at its root, which herdr
	// clamps to 0.1. The UI says so rather than silently lying.
	chain := Leaf("p0")
	for i := 1; i < 12; i++ {
		chain = r(Leaf(paneName(i)), chain)
	}
	if BalanceIsExact(chain) {
		t.Error("a 12-pane chain cannot be balanced within herdr's clamp")
	}
	if got := Balance(chain); got.Ratio != MinRatio {
		t.Errorf("root ratio = %v, want the clamp %v", got.Ratio, MinRatio)
	}

	// The same twelve panes stacked the other way at each step are one cell per
	// split, so they balance exactly.
	zigzag := Leaf("p0")
	for i := 1; i < 12; i++ {
		if i%2 == 0 {
			zigzag = r(Leaf(paneName(i)), zigzag)
		} else {
			zigzag = d(Leaf(paneName(i)), zigzag)
		}
	}
	if !BalanceIsExact(zigzag) {
		t.Error("alternating splits are one cell each side, so they balance exactly")
	}
	if !BalanceIsExact(Leaf("only")) {
		t.Error("a single pane is trivially exact")
	}
}

func TestCellsCountsRunsAlongOneAxis(t *testing.T) {
	n := Split(herdr.Right, 0.5,
		Split(herdr.Right, 0.5, Leaf("a"), Leaf("b")),
		Split(herdr.Down, 0.5, Leaf("c"), Leaf("d")))

	if got := cells(n, herdr.Right); got != 3 {
		t.Errorf("cells along right = %d, want 3", got)
	}
	// Along the other axis the whole tab is a single cell.
	if got := cells(n, herdr.Down); got != 1 {
		t.Errorf("cells along down = %d, want 1", got)
	}
	if got := cells(Leaf("a"), herdr.Right); got != 1 {
		t.Errorf("a pane is %d cells, want 1", got)
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
