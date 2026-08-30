package tree

import (
	"testing"
)

func panes(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = paneName(i)
	}
	return out
}

// The golden shapes for every preset at every interesting pane count. "|" is a
// right split, "/" a down split; p0 is the main pane.
func TestPresetShapes(t *testing.T) {
	golden := map[Preset][]string{
		EvenHorizontal: {
			"p0",
			"[p0 | p1]",
			"[p0 | [p1 | p2]]",
			"[[p0 | p1] | [p2 | p3]]",
			"[[p0 | p1] | [p2 | [p3 | p4]]]",
			"[[p0 | [p1 | p2]] | [p3 | [p4 | p5]]]",
			"[[p0 | [p1 | p2]] | [[p3 | p4] | [p5 | p6]]]",
			"[[[p0 | p1] | [p2 | p3]] | [[p4 | p5] | [p6 | p7]]]",
		},
		EvenVertical: {
			"p0",
			"[p0 / p1]",
			"[p0 / [p1 / p2]]",
			"[[p0 / p1] / [p2 / p3]]",
			"[[p0 / p1] / [p2 / [p3 / p4]]]",
			"[[p0 / [p1 / p2]] / [p3 / [p4 / p5]]]",
			"[[p0 / [p1 / p2]] / [[p3 / p4] / [p5 / p6]]]",
			"[[[p0 / p1] / [p2 / p3]] / [[p4 / p5] / [p6 / p7]]]",
		},
		MainHorizontal: {
			"p0",
			"[p0 / p1]",
			"[p0 / [p1 | p2]]",
			"[p0 / [p1 | [p2 | p3]]]",
			"[p0 / [[p1 | p2] | [p3 | p4]]]",
			"[p0 / [[p1 | p2] | [p3 | [p4 | p5]]]]",
			"[p0 / [[p1 | [p2 | p3]] | [p4 | [p5 | p6]]]]",
			"[p0 / [[p1 | [p2 | p3]] | [[p4 | p5] | [p6 | p7]]]]",
		},
		MainVertical: {
			"p0",
			"[p0 | p1]",
			"[p0 | [p1 / p2]]",
			"[p0 | [p1 / [p2 / p3]]]",
			"[p0 | [[p1 / p2] / [p3 / p4]]]",
			"[p0 | [[p1 / p2] / [p3 / [p4 / p5]]]]",
			"[p0 | [[p1 / [p2 / p3]] / [p4 / [p5 / p6]]]]",
			"[p0 | [[p1 / [p2 / p3]] / [[p4 / p5] / [p6 / p7]]]]",
		},
		Tiled: {
			"p0",
			"[p0 | p1]",
			"[[p0 | p1] / p2]",
			"[[p0 | p1] / [p2 | p3]]",
			"[[p0 | [p1 | p2]] / [p3 | p4]]",
			"[[p0 | [p1 | p2]] / [p3 | [p4 | p5]]]",
			"[[p0 | [p1 | p2]] / [[p3 | [p4 | p5]] / p6]]",
			"[[p0 | [p1 | p2]] / [[p3 | [p4 | p5]] / [p6 | p7]]]",
		},
	}

	for _, preset := range Presets {
		t.Run(preset.Name(), func(t *testing.T) {
			for i, want := range golden[preset] {
				n := i + 1
				got := preset.Build(panes(n), "p0")
				if got.String() != want {
					t.Errorf("n=%d: got %s, want %s", n, got, want)
				}
				if got.Count() != n {
					t.Errorf("n=%d: holds %d panes", n, got.Count())
				}
			}
		})
	}
}

// Every pane should get the same share of the tab, which is the point of the
// presets. Nested ratios multiply, so this checks the area each leaf ends up
// with rather than the ratios themselves.
func TestPresetsGiveEveryPaneAnEqualShare(t *testing.T) {
	for _, preset := range Presets {
		t.Run(preset.Name(), func(t *testing.T) {
			for n := 1; n <= 9; n++ {
				tree := preset.Build(panes(n), "p0")
				areas := leafAreas(tree)

				if preset == MainHorizontal || preset == MainVertical {
					// The main pane deliberately takes half the tab.
					if n > 1 && abs(areas["p0"]-0.5) > 0.001 {
						t.Errorf("n=%d: main pane has %.3f of the tab, want 0.5", n, areas["p0"])
					}
					continue
				}

				want := 1.0 / float64(n)
				for pane, area := range areas {
					if abs(area-want) > 0.001 {
						t.Errorf("n=%d: pane %s has %.4f of the tab, want %.4f", n, pane, area, want)
					}
				}
			}
		})
	}
}

func TestPresetsKeepRatiosInsideTheClamp(t *testing.T) {
	// A preset asking for a ratio herdr will not store would render wrong.
	for _, preset := range Presets {
		for n := 1; n <= 16; n++ {
			for _, ra := range preset.Build(panes(n), "p0").Ratios() {
				if ra.Ratio < MinRatio || ra.Ratio > MaxRatio {
					t.Errorf("%s n=%d: ratio %v at %v is outside [%v, %v]",
						preset.Name(), n, ra.Ratio, ra.Path, MinRatio, MaxRatio)
				}
			}
		}
	}
}

func TestMainPresetsUseTheGivenPane(t *testing.T) {
	// `4` should make *this* pane the big one, wherever it sits in the tab.
	for _, preset := range []Preset{MainHorizontal, MainVertical} {
		for _, main := range panes(4) {
			got := preset.Build(panes(4), main)
			if got.First.PaneID != main {
				t.Errorf("%s main=%s: first leaf is %s", preset.Name(), main, got.First.PaneID)
			}
			if !sameSet(got.Leaves(), panes(4)) {
				t.Errorf("%s main=%s: panes = %v", preset.Name(), main, got.Leaves())
			}
		}
	}
}

func TestMainPresetFallsBackWhenMainIsNotInTheTab(t *testing.T) {
	// A pane can close between reading the tab and pressing a key; the preset
	// must still lay out every pane that is there rather than dropping one.
	got := MainVertical.Build(panes(3), "gone")
	if got.Count() != 3 || !sameSet(got.Leaves(), panes(3)) {
		t.Errorf("Build = %s, want all three panes", got)
	}
	if got.First.PaneID != "p0" {
		t.Errorf("first leaf = %s, want the fallback p0", got.First.PaneID)
	}
}

func TestBuildHandlesEmptyAndSinglePane(t *testing.T) {
	for _, preset := range Presets {
		if got := preset.Build(nil, ""); got != nil {
			t.Errorf("%s: Build(nil) = %s, want nil", preset.Name(), got)
		}
		if got := preset.Build([]string{"only"}, "only"); got.String() != "only" {
			t.Errorf("%s: Build(one pane) = %s", preset.Name(), got)
		}
	}
}

func TestDetectRecognisesItsOwnOutput(t *testing.T) {
	// Detect drives the status line and `space`, so every preset must recognise
	// the layout it just produced.
	for _, preset := range Presets {
		for n := 3; n <= 9; n++ {
			tree := preset.Build(panes(n), "p0")
			got, ok := Detect(tree)
			if !ok {
				t.Errorf("%s n=%d: not detected (%s)", preset.Name(), n, tree)
				continue
			}
			// Presets can coincide at small pane counts; require that whatever was
			// detected renders the same shape, not that it is the same enum value.
			if !SameShape(tree, got.Build(panes(n), "p0")) {
				t.Errorf("%s n=%d: detected %s, which is a different layout", preset.Name(), n, got.Name())
			}
		}
	}
}

func TestDetectIgnoresRatios(t *testing.T) {
	// A layout the user has since resized by dragging should still be recognised.
	tree := EvenHorizontal.Build(panes(4), "p0")
	tree = SetRatioAt(tree, nil, 0.8)
	if _, ok := Detect(tree); !ok {
		t.Error("a resized preset should still be detected")
	}
}

func TestDetectRejectsAHandBuiltTree(t *testing.T) {
	// [[p0 | p1] | p2] is not any of the presets at n=3.
	odd := r(r(Leaf("p0"), Leaf("p1")), Leaf("p2"))
	if p, ok := Detect(odd); ok {
		t.Errorf("Detect(%s) = %s, want no match", odd, p.Name())
	}
	if _, ok := Detect(nil); ok {
		t.Error("Detect(nil) should not match")
	}
}

func TestDetectForDependsOnTheMainPane(t *testing.T) {
	// The main-* presets mean "this pane is the big one", so which pane is being
	// asked about changes the answer. The UI relies on this: it detects against the
	// pane the user is arranging, so the name it shows is the one a number key
	// would reproduce.
	main := MainVertical.Build(panes(3), "p0")

	if got, ok := DetectFor(main, "p0"); !ok || got != MainVertical {
		t.Errorf("DetectFor(%s, p0) = %s, %v", main, got.Name(), ok)
	}
	if got, ok := DetectFor(main, "p1"); ok {
		t.Errorf("DetectFor(%s, p1) = %s, want no match", main, got.Name())
	}

	// The even-* and tiled presets ignore the main pane entirely.
	even := EvenHorizontal.Build(panes(4), "p0")
	for _, pane := range panes(4) {
		if got, ok := DetectFor(even, pane); !ok || got != EvenHorizontal {
			t.Errorf("DetectFor(%s, %s) = %s, %v", even, pane, got.Name(), ok)
		}
	}

	if _, ok := DetectFor(nil, "p0"); ok {
		t.Error("DetectFor(nil) should not match")
	}
}

func TestNextCyclesAndAlwaysChangesSomething(t *testing.T) {
	// Pressing space repeatedly must keep changing the layout: never stall on a
	// preset that happens to render identically to the current one.
	for n := 2; n <= 8; n++ {
		tree := EvenHorizontal.Build(panes(n), "p0")
		seen := map[string]bool{tree.String(): true}

		for press := 1; press <= 12; press++ {
			preset := Next(tree, "p0")
			next := preset.Build(tree.Leaves(), "p0")
			if SameShape(next, tree) {
				t.Fatalf("n=%d press %d: Next returned %s, which changes nothing", n, press, preset.Name())
			}
			tree = next
			seen[tree.String()] = true
		}
		// Twelve presses over five presets must have produced several layouts.
		if len(seen) < 2 {
			t.Errorf("n=%d: cycling produced only %d distinct layouts", n, len(seen))
		}
	}
}

func TestNextVisitsEveryDistinctPreset(t *testing.T) {
	// With enough panes all five presets are distinct, and cycling should reach
	// all of them rather than looping between two.
	const n = 6
	tree := EvenHorizontal.Build(panes(n), "p0")
	visited := map[string]bool{}
	for range 10 {
		preset := Next(tree, "p0")
		visited[preset.Name()] = true
		tree = preset.Build(tree.Leaves(), "p0")
	}
	for _, p := range Presets {
		if !visited[p.Name()] {
			t.Errorf("cycling never reached %s (reached %v)", p.Name(), visited)
		}
	}
}

func TestNextOnAnUnrecognisedLayoutStartsAtTheFirstPreset(t *testing.T) {
	odd := r(r(Leaf("p0"), Leaf("p1")), Leaf("p2"))
	if got := Next(odd, "p0"); got != EvenHorizontal {
		t.Errorf("Next = %s, want even-horizontal", got.Name())
	}
	if got := Next(nil, ""); got != EvenHorizontal {
		t.Errorf("Next(nil) = %s, want even-horizontal", got.Name())
	}
}

func TestPresetNames(t *testing.T) {
	want := []string{"even-horizontal", "even-vertical", "main-horizontal", "main-vertical", "tiled"}
	for i, p := range Presets {
		if p.Name() != want[i] {
			t.Errorf("Presets[%d].Name() = %q, want %q", i, p.Name(), want[i])
		}
	}
	if got := Preset(99).Name(); got != "custom" {
		t.Errorf("unknown preset name = %q", got)
	}
}

// leafAreas returns the fraction of the tab each pane occupies.
func leafAreas(n *Node) map[string]float64 {
	out := map[string]float64{}
	var rec func(*Node, float64)
	rec = func(cur *Node, area float64) {
		if cur == nil {
			return
		}
		if cur.IsLeaf() {
			out[cur.PaneID] = area
			return
		}
		rec(cur.First, area*cur.Ratio)
		rec(cur.Second, area*(1-cur.Ratio))
	}
	rec(n, 1.0)
	return out
}
