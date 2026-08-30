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

// hugeSession is a session whose tree is longer than any popup: 40 more workspaces,
// a tab and a pane each.
func hugeSession() *herdr.SessionSnapshot {
	s := fixtureSnapshot(evenFour().Leaves())
	for i := range 40 {
		ws := fmt.Sprintf("w%dZ", i)
		s.Workspaces = append(s.Workspaces, herdr.WorkspaceInfo{WorkspaceID: ws, Number: i + 3})
		s.Tabs = append(s.Tabs, herdr.TabInfo{TabID: ws + ":t1", WorkspaceID: ws, PaneCount: 1})
		s.Panes = append(s.Panes, herdr.PaneInfo{PaneID: ws + ":p1", TabID: ws + ":t1", WorkspaceID: ws})
	}
	return s
}

// TestTreePopupSizeIsBoundedBothWays: a tiny tree asks for layout mode's size rather
// than its own, so switching views costs nothing — and a huge session must not ask for
// a popup taller than the screen it has to fit on.
func TestTreePopupSizeIsBoundedBothWays(t *testing.T) {
	_, layoutHeight := LayoutPopupSize()

	small := fixtureSnapshot([]string{curPane})
	small.Workspaces, small.Tabs = small.Workspaces[:1], small.Tabs[:1]
	small.Panes = small.Panes[:1]
	if _, height := TreePopupSize(small, curPane, curTab, curWS); height != layoutHeight {
		t.Errorf("a one-pane session asks for height %d, want layout mode's %d", height, layoutHeight)
	}

	big := hugeSession()
	room := popupRoom(big)
	if room == 0 {
		t.Fatal("the fixture stopped reporting the screen size, so this tests the fallback instead")
	}
	if _, height := TreePopupSize(big, curPane, curTab, curWS); height != room-treePopupScreenMargin {
		t.Errorf("a 40-workspace session asks for height %d on a %d-line screen", height, room)
	}

	// Whatever it asked for, the view fits it and still scrolls to the cursor.
	f := newFakeClient(evenFour())
	f.snapshot = big
	width, height := inner(TreePopupSize(big, curPane, curTab, curWS))
	m := send(t, start(t, f, ModeTree), sizeMsg(width, height))
	if got := lines(m); len(got) != height {
		t.Errorf("the tree is %d lines inside a %d-line popup", len(got), height)
	}
	if at := m.cursorLine(); at < m.vp.YOffset || at >= m.vp.YOffset+m.vp.Height {
		t.Errorf("the cursor at %d is outside rows %d..%d", at, m.vp.YOffset, m.vp.YOffset+m.vp.Height)
	}
}

// TestTreePopupSizeFollowsTheScreen: the popup can only be as tall as the area herdr
// draws in, and that area is the one thing the snapshot does say about the terminal.
// The blind cap is the fallback for a herdr that does not report it.
func TestTreePopupSizeFollowsTheScreen(t *testing.T) {
	big := hugeSession()

	for _, screen := range []int{24, 50, 90} {
		big.Layouts = []herdr.LayoutSnapshot{{TabID: curTab, Area: herdr.LayoutRect{Width: 200, Height: screen}}}
		_, height := TreePopupSize(big, curPane, curTab, curWS)

		_, floor := LayoutPopupSize()
		want := max(screen-treePopupScreenMargin, floor)
		if height != want {
			t.Errorf("on a %d-line screen tree mode asks for %d, want %d", screen, height, want)
		}
	}

	big.Layouts = nil
	if _, height := TreePopupSize(big, curPane, curTab, curWS); height != treePopupMaxHeight {
		t.Errorf("with no screen size reported tree mode asks for %d, want the cap %d", height, treePopupMaxHeight)
	}
}

// TestTheUIAgreesWithTheActionAboutTheTreesSize: the action sizes the popup from the
// snapshot and the UI decides whether to reopen from its own copy of it. If the two
// ever read it differently, switching into tree mode would reopen the popup at a size
// it then thinks it has outgrown — forever.
func TestTheUIAgreesWithTheActionAboutTheTreesSize(t *testing.T) {
	for _, s := range []*herdr.SessionSnapshot{fixtureSnapshot(evenFour().Leaves()), hugeSession()} {
		f := newFakeClient(evenFour())
		f.snapshot = s
		m := start(t, f, ModeTree)

		if got, want := sizeOf(treeSizeForRows(len(m.rows), m.room)), sizeOf(TreePopupSize(s, curPane, curTab, curWS)); got != want {
			t.Errorf("the UI wants %v for %d rows, the action asked for %v", got, len(m.rows), want)
		}
	}
}

// popupSized starts the UI the way the action does: in a popup of the size that
// mode asked for.
func popupSized(t *testing.T, f *fakeClient, mode Mode) Model {
	t.Helper()

	width, height := LayoutPopupSize()
	if mode == ModeTree {
		width, height = TreePopupSize(f.snapshot, curPane, curTab, curWS)
	}
	m := startWith(t, f, Options{Mode: mode, AskedWidth: width, AskedHeight: height})
	return send(t, m, sizeMsg(inner(width, height)))
}

// oneTabSession is a session whose tree is short enough to fit layout mode's own
// popup, which is what lets `t` switch views without reopening anything.
func oneTabSession(panes ...string) *herdr.SessionSnapshot {
	s := fixtureSnapshot(panes)
	s.Workspaces, s.Tabs, s.Panes = s.Workspaces[:1], s.Tabs[:1], s.Panes[:len(panes)]
	return s
}

// TestSwitchingViewsReopensThePopupForTheOtherSize is the whole point of Reopen:
// herdr cannot resize a popup, so a view that does not fit the one we are in gets a
// new one instead of being squeezed into or lost inside the old one.
func TestSwitchingViewsReopensThePopupForTheOtherSize(t *testing.T) {
	for _, c := range []struct {
		from Mode
		want Mode
	}{{ModeLayout, ModeTree}, {ModeTree, ModeLayout}} {
		t.Run(modeWord(c.want), func(t *testing.T) {
			f := newFakeClient(evenFour())
			f.snapshot = hugeSession() // a tree far taller than the layout panel
			m := press(t, popupSized(t, f, c.from), "t")

			if m.reopen == nil {
				t.Fatalf("switching to %s kept a popup built for %s", modeWord(c.want), modeWord(c.from))
			}
			if m.reopen.Mode != c.want {
				t.Errorf("the replacement opens in %s", modeWord(m.reopen.Mode))
			}
			if !m.quitting {
				t.Error("the popup stayed open, so its replacement can never open")
			}
			// Nothing is drawn at the old size on the way out.
			if m.View() != "" {
				t.Errorf("a popup being replaced still drew something:\n%s", plain(m.View()))
			}
		})
	}
}

// TestSwitchingViewsInAPopupThatFitsBothIsInstant: reopening costs a flicker, so a
// session small enough that both views fit the popup we already have must not pay it.
func TestSwitchingViewsInAPopupThatFitsBothIsInstant(t *testing.T) {
	f := newFakeClient(tree.Split(herdr.Right, 0.5, tree.Leaf(curPane), tree.Leaf("w1S:p1")))
	f.snapshot = oneTabSession(curPane, "w1S:p1")

	if want, got := sizeOf(LayoutPopupSize()), sizeOf(TreePopupSize(f.snapshot, curPane, curTab, curWS)); want != got {
		t.Fatalf("this session's tree wants %v, not layout mode's %v: the fixture no longer tests anything", got, want)
	}

	m := popupSized(t, f, ModeLayout)
	for _, want := range []Mode{ModeTree, ModeLayout, ModeTree} {
		m = press(t, m, "t")
		if m.reopen != nil {
			t.Fatalf("switching to %s reopened a popup that already fits it", modeWord(want))
		}
		if m.mode != want || m.busy {
			t.Fatalf("mode is %s, busy=%v; want %s", modeWord(m.mode), m.busy, modeWord(want))
		}
	}
}

// TestAHandRunUINeverReopens: run outside a popup there is no popup to replace, and
// asking for one would put a second copy of the UI on screen.
func TestAHandRunUINeverReopens(t *testing.T) {
	f := newFakeClient(evenFour())
	m := press(t, start(t, f, ModeTree), "t") // start says nothing about a popup size

	if m.reopen != nil || m.quitting {
		t.Fatalf("reopen=%+v quitting=%v", m.reopen, m.quitting)
	}
	if m.mode != ModeLayout {
		t.Error("the view did not switch")
	}
}

// TestAReopenCarriesTheResultOver: an action that switches views is the one that most
// wants to report what it did, and closing the popup would otherwise take the report
// with it.
func TestAReopenCarriesTheResultOver(t *testing.T) {
	f := newFakeClient(evenFour())
	f.snapshot = hugeSession()                     // so tree mode is in a popup layout mode cannot use
	m := press(t, popupSized(t, f, ModeTree), "c") // move to a new tab, then arrange it

	if !f.took("move w1S:p2 -> new_tab") {
		t.Fatalf("calls were: %s", f.log())
	}
	if m.reopen == nil {
		t.Fatal("the popup was not resized for layout mode")
	}
	if m.reopen.Status != "moved to a new tab" {
		t.Errorf("the replacement is told %q", m.reopen.Status)
	}

	// And the popup that replaces it says so.
	m = startWith(t, newFakeClient(tree.Leaf(curPane)), Options{Mode: ModeLayout, Status: m.reopen.Status})
	if m.statusKind != statusInfo {
		t.Errorf("a carried result is styled as %v", m.statusKind)
	}
	if !strings.Contains(plain(m.View()), "moved to a new tab") {
		t.Errorf("the carried result is not on screen:\n%s", plain(m.View()))
	}
}

// TestAClampedPopupIsNotAResize: herdr shrinks a popup to fit the terminal, and a UI
// that took its own window as the size it asked for would reopen itself forever.
func TestAClampedPopupIsNotAResize(t *testing.T) {
	f := newFakeClient(evenFour())
	width, height := LayoutPopupSize()
	m := startWith(t, f, Options{Mode: ModeLayout, AskedWidth: width, AskedHeight: height})

	// A terminal with half the room we asked for.
	m = send(t, m, sizeMsg(width/2, height/2))
	if m.outgrewThePopup(LayoutPopupSize()) {
		t.Fatal("the view thinks it has outgrown the popup it was built for")
	}
}

// modeWord names a mode for a test message.
func modeWord(mode Mode) string {
	if mode == ModeTree {
		return "tree mode"
	}
	return "layout mode"
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
