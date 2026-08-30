package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

// inner is what the program gets to draw in, given the outer popup size the
// action asks herdr for.
func inner(width, height int) (int, int) {
	return width - popupBorderWidth, height - popupBorderHeight
}

// TestLayoutPopupSizeFitsTheHelpPanel checks the number the action asks for
// against the panel actually rendered inside it, border included.
func TestLayoutPopupSizeFitsTheHelpPanel(t *testing.T) {
	f := newFakeClient(evenFour())
	width, height := inner(LayoutPopupSize())
	m := send(t, start(t, f, ModeLayout), sizeMsg(width, height))

	got := lines(m)
	if len(got) != height {
		t.Errorf("the panel is %d lines inside a %d-line popup:\n%s", len(got), height, plain(m.View()))
	}
	// Nothing clipped: the first help line and both status lines are all there.
	for _, want := range []string{"swap pane", "enter / esc", "this pane: p2"} {
		if !strings.Contains(plain(m.View()), want) {
			t.Errorf("the panel is missing %q:\n%s", want, plain(m.View()))
		}
	}
}

// TestTreePopupSizeShowsTheWholeSession is the point of sizing the popup from the
// snapshot: for an ordinary session the tree needs no scrolling.
func TestTreePopupSizeShowsTheWholeSession(t *testing.T) {
	f := newFakeClient(evenFour())
	m := start(t, f, ModeTree)

	width, height := inner(TreePopupSize(f.snapshot, curPane, curTab, curWS))
	m = send(t, m, sizeMsg(width, height))

	if m.vp.Height < len(m.rows) {
		t.Errorf("%d rows in a %d-line viewport", len(m.rows), m.vp.Height)
	}
	if !strings.Contains(plain(m.View()), "new workspace") {
		t.Errorf("the last row is off screen:\n%s", plain(m.View()))
	}
}

// TestTreePopupSizeIsBoundedBothWays: `t` switches views inside the popup we were
// given, so a tiny tree must not leave layout mode without room — and a huge
// session must not ask for a popup the height of the screen.
func TestTreePopupSizeIsBoundedBothWays(t *testing.T) {
	_, layoutHeight := LayoutPopupSize()

	small := fixtureSnapshot([]string{curPane})
	small.Workspaces, small.Tabs = small.Workspaces[:1], small.Tabs[:1]
	small.Panes = small.Panes[:1]
	if _, height := TreePopupSize(small, curPane, curTab, curWS); height != layoutHeight {
		t.Errorf("a one-pane session asks for height %d, want layout mode's %d", height, layoutHeight)
	}

	big := fixtureSnapshot(evenFour().Leaves())
	for i := range 40 {
		ws := fmt.Sprintf("w%dZ", i)
		big.Workspaces = append(big.Workspaces, herdr.WorkspaceInfo{WorkspaceID: ws, Number: i + 3})
		big.Tabs = append(big.Tabs, herdr.TabInfo{TabID: ws + ":t1", WorkspaceID: ws, PaneCount: 1})
		big.Panes = append(big.Panes, herdr.PaneInfo{PaneID: ws + ":p1", TabID: ws + ":t1", WorkspaceID: ws})
	}
	if _, height := TreePopupSize(big, curPane, curTab, curWS); height != treePopupMaxHeight {
		t.Errorf("a 40-workspace session asks for height %d, want the cap %d", height, treePopupMaxHeight)
	}

	// Whatever it asked for, the view fits it and still scrolls to the cursor.
	f := newFakeClient(evenFour())
	f.snapshot = big
	width, height := inner(TreePopupSize(big, curPane, curTab, curWS))
	m := send(t, start(t, f, ModeTree), sizeMsg(width, height))
	if got := lines(m); len(got) != height {
		t.Errorf("the tree is %d lines inside a %d-line popup", len(got), height)
	}
	if m.cursor < m.vp.YOffset || m.cursor >= m.vp.YOffset+m.vp.Height {
		t.Errorf("the cursor at %d is outside rows %d..%d", m.cursor, m.vp.YOffset, m.vp.YOffset+m.vp.Height)
	}
}

// TestPopupSizesClearHerdrsMinimum: below 6x4 herdr refuses to open a popup at
// all, so neither mode may ask for less.
func TestPopupSizesClearHerdrsMinimum(t *testing.T) {
	sizes := map[string][2]int{}
	sizes["layout"] = sizeOf(LayoutPopupSize())
	sizes["tree"] = sizeOf(TreePopupSize(fixtureSnapshot(tree.Leaf(curPane).Leaves()), curPane, curTab, curWS))

	for mode, size := range sizes {
		if size[0] < 6 || size[1] < 4 {
			t.Errorf("%s mode asks for %dx%d, which herdr will refuse", mode, size[0], size[1])
		}
	}
}

func sizeOf(width, height int) [2]int { return [2]int{width, height} }
