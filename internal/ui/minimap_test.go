package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

// mapOf renders the minimap for a tab, unstyled and without the leading margin, so
// a golden reads as the picture the user sees.
func mapOf(t *testing.T, root *tree.Node) string {
	t.Helper()

	m := start(t, newFakeClient(root), ModeLayout)
	var out []string
	for _, line := range m.minimap() {
		out = append(out, strings.TrimPrefix(plain(line), " "))
	}
	return strings.Join(out, "\n")
}

// TestMinimapDrawsTheTab is the picture itself: four equal columns, with the pane
// being arranged named and measured inside its own box.
//
// The junction runes are the point of drawing through an edge mask rather than
// writing runes as each box is walked: where two panes meet the tab's own border the
// result has to be ┬ and ┴, whichever order the panes came back in.
func TestMinimapDrawsTheTab(t *testing.T) {
	want := strings.Join([]string{
		"┌──────┬──────┬─────┬──────┐",
		"│      │      │     │      │",
		"│      │w1S:p2│     │      │",
		"│      │84x84 │     │      │",
		"│      │      │     │      │",
		"│      │      │     │      │",
		"└──────┴──────┴─────┴──────┘",
	}, "\n")

	if got := mapOf(t, evenFour()); got != want {
		t.Errorf("the map of an even-horizontal tab is\n%s\nwant\n%s", got, want)
	}
}

// TestMinimapShowsTheShapeOfTheTab: the whole reason to draw a picture is that the
// status line's "main-vertical" does not say which side the column is on, nor that
// the other side is split in two.
func TestMinimapShowsTheShapeOfTheTab(t *testing.T) {
	mainVertical := tree.Split(herdr.Right, 0.6,
		tree.Leaf(curPane),
		tree.Split(herdr.Down, 0.5, tree.Leaf("w1S:p3"), tree.Leaf("w1S:p4")),
	)
	want := strings.Join([]string{
		"┌───────────────┬──────────┐",
		"│               │          │",
		"│    w1S:p2     │          │",
		"│    202x84     ├──────────┤",
		"│               │          │",
		"│               │          │",
		"└───────────────┴──────────┘",
	}, "\n")

	if got := mapOf(t, mainVertical); got != want {
		t.Errorf("the map of a main-vertical tab is\n%s\nwant\n%s", got, want)
	}
}

// TestMinimapIsAsWideAsTheTab: map cells and screen cells are the same shape, so a
// tab twice as wide as it is tall has to draw twice as wide as it is tall. A map
// stretched to the panel's width would be a picture of a tab nobody has.
func TestMinimapIsAsWideAsTheTab(t *testing.T) {
	cases := []struct {
		area herdr.LayoutRect
		want int
	}{
		{herdr.LayoutRect{Width: 336, Height: 84}, 28}, // 4:1, the fixture screen
		{herdr.LayoutRect{Width: 80, Height: 40}, 14},  // 2:1
		{herdr.LayoutRect{Width: 40, Height: 100}, 12}, // taller than wide: the floor
		{herdr.LayoutRect{Width: 800, Height: 40}, 56}, // very wide: the panel's width
		{herdr.LayoutRect{Width: 0, Height: 0}, 56},    // no area reported
	}

	for _, c := range cases {
		width, height := minimapSize(c.area)
		if width != c.want || height != minimapRows {
			t.Errorf("a %dx%d tab draws %dx%d, want %dx%d",
				c.area.Width, c.area.Height, width, height, c.want, minimapRows)
		}
	}
}

// TestMinimapLeavesABoxTooSmallToLabelEmpty: a box four rows of the map high has
// room for both lines, one two rows high has room for neither, and "w1S…" in a box
// says less than an empty box does.
func TestMinimapLeavesABoxTooSmallToLabelEmpty(t *testing.T) {
	// Six equal rows in a seven-row map: every box is a border, a border, and no
	// room at all between them.
	panes := []string{"w1S:p1", curPane, "w1S:p3", "w1S:p4", "w1S:p5", "w1S:p6"}
	stack := tree.Leaf(panes[len(panes)-1])
	for i := len(panes) - 2; i >= 0; i-- {
		left := len(panes) - i // rows still to share out, this pane included
		stack = tree.Split(herdr.Down, 1/float64(left), tree.Leaf(panes[i]), stack)
	}

	got := mapOf(t, stack)
	if strings.Contains(got, "p2") || strings.Contains(got, "x") {
		t.Errorf("a box with no room in it was labelled anyway:\n%s", got)
	}
	// It is still a picture: six panes stacked means every row is a boundary.
	if lines := strings.Split(got, "\n"); len(lines) != minimapRows {
		t.Errorf("the map is %d rows:\n%s", len(lines), got)
	}
}

// TestMinimapReportsWhyThereIsNoPicture: pane.layout failing costs the map and
// nothing else, so the popup says so in the map's own space and stays usable. Putting
// it in the status line would overwrite whatever the last action reported, and would
// do it again after every reload.
func TestMinimapReportsWhyThereIsNoPicture(t *testing.T) {
	f := newFakeClient(evenFour())
	f.geoErr = errors.New("layout unavailable")
	m := send(t, start(t, f, ModeLayout), sizeMsg(inner(LayoutPopupSize())))

	view := plain(m.View())
	if !strings.Contains(view, "no map: layout unavailable") {
		t.Errorf("the view does not say why there is no map:\n%s", view)
	}
	// The rest of the popup is untouched: the keys still work and still say so.
	for _, want := range []string{"resize pane", "this pane: p2"} {
		if !strings.Contains(view, want) {
			t.Errorf("the view is missing %q:\n%s", want, view)
		}
	}
	if m.statusKind == statusFail {
		t.Errorf("a missing map was reported as a failure: %q", m.status)
	}
}

// TestMinimapBlockIsAlwaysTheSameHeight is what keeps the help from jumping up and
// down the popup as the map changes: one pane, four panes or no map at all, the block
// above the help is the same number of lines.
func TestMinimapBlockIsAlwaysTheSameHeight(t *testing.T) {
	one := newFakeClient(tree.Leaf(curPane))

	broken := newFakeClient(evenFour())
	broken.geoErr = errors.New("nope")

	for _, f := range []*fakeClient{newFakeClient(evenFour()), one, broken} {
		m := start(t, f, ModeLayout)
		if got := len(m.minimapBlock()); got != minimapHeight {
			t.Errorf("the map block is %d lines, want %d", got, minimapHeight)
		}
	}
}

// TestMinimapNeedsRoomOrGoesAway: herdr clamps an oversized popup down to the
// terminal, and in a short one the help matters more than the picture of what it acts
// on. Half a map would be a lie about where the panes are, so it is all or nothing.
func TestMinimapNeedsRoomOrGoesAway(t *testing.T) {
	f := newFakeClient(evenFour())
	m := start(t, f, ModeLayout)

	full := plain(send(t, m, sizeMsg(inner(LayoutPopupSize()))).View())
	if !strings.Contains(full, "w1S:p2") || !strings.Contains(full, "84x84") {
		t.Errorf("the popup the action asks for has no map in it:\n%s", full)
	}

	short := plain(send(t, m, sizeMsg(60, layoutPanelHeight-minimapHeight)).View())
	if strings.Contains(short, "┌") {
		t.Errorf("a popup with no room for the map drew one anyway:\n%s", short)
	}
	if !strings.Contains(short, "swap pane") || !strings.Contains(short, "this pane: p2") {
		t.Errorf("the help gave way before the map did:\n%s", short)
	}
}
