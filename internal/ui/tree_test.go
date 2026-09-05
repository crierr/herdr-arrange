package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/crierr/herdr-arrange/internal/tree"
)

// treeFixture opens tree mode on the fixture session, unfolded to the panes and tall
// enough to show every row at once.
func treeFixture(t *testing.T) (*fakeClient, Model) {
	t.Helper()
	f := newFakeClient(evenFour())
	m := start(t, f, ModeTree)
	return f, send(t, m, sizeMsg(66, 30), key(t, "l"))
}

// rowAt finds the index of the first row matching a predicate.
func rowAt(t *testing.T, m Model, what string, match func(row) bool) int {
	t.Helper()
	for i, r := range m.rows {
		if match(r) {
			return i
		}
	}
	t.Fatalf("no %s row in:\n%s", what, plain(m.View()))
	return -1
}

// selectRow puts the cursor on a row, unfolding the tree far enough to show it.
func selectRow(t *testing.T, m Model, what string, match func(row) bool) Model {
	t.Helper()
	m.cursor = rowAt(t, m, what, match)
	m.want = m.rows[m.cursor].key()
	m.level = max(m.level, expandLevel(m.rows[m.cursor].depth()))
	m.syncViewport()
	return m
}

// selectedLine is the line the cursor is drawn on, as rendered.
func selectedLine(m Model) string { return lines(m)[m.cursorLine()-m.vp.YOffset] }

// TestTreeViewShowsTheWholeSession is the golden for the tree drawing, unfolded: the
// connectors, the ids, the labels, the pane counts, the current-pane marker, the
// action preview beside the selected row and the two synthetic action rows.
func TestTreeViewShowsTheWholeSession(t *testing.T) {
	_, m := treeFixture(t)

	// The workspace rows start a column to the left of the tabs under them, so the
	// number in "[1]" lines up with their connectors.
	want := []string{
		"  [1] w1S  herdr-arrange",
		"   ├─ t1  main  4 panes  (current)",
		"   │  ├─ p1",
		" ▸ │  ├─ p2  claude  (current)  ← back to layout mode",
		"   │  ├─ p3",
		"   │  └─ p4",
		"   ├─ t2  logs  1 pane",
		"   │  └─ p7  tail",
		"   └─ [c] new tab in this workspace",
		"  [2] wJ  notes",
		"   └─ t1  1 pane",
		"      └─ p1",
		"  [N] new workspace",
	}

	got := lines(m)
	for i, wantLine := range want {
		if i >= len(got) {
			t.Fatalf("the view has only %d lines, want at least %d", len(got), len(want))
		}
		if trimmed := strings.TrimRight(got[i], " "); trimmed != wantLine {
			t.Errorf("line %d:\n got %q\nwant %q", i+1, trimmed, wantLine)
		}
	}
	if n := len(m.rows); n != len(want) {
		t.Errorf("the tree has %d rows, want %d", n, len(want))
	}
}

// TestTreeCursorStartsOnTheArrangedPane: the tree exists to move one pane, so that
// pane is where the eye should start — folded to the tabs that means its tab, and
// unfolding gets to the pane itself without the user having to go looking for it.
func TestTreeCursorStartsOnTheArrangedPane(t *testing.T) {
	f := newFakeClient(evenFour())
	m := send(t, start(t, f, ModeTree), sizeMsg(66, 30))

	if r := m.rows[m.cursor]; r.kind != rowTab || r.tabID != curTab {
		t.Fatalf("the cursor starts on %+v", r)
	}

	m = press(t, m, "l")
	if r := m.rows[m.cursor]; r.paneID != curPane {
		t.Fatalf("unfolding left the cursor on %+v", r)
	}
	if !strings.Contains(plain(m.View()), "← back to layout mode") {
		t.Errorf("preview is %q", m.treePreview())
	}
}

// TestTreeFoldsToThreeLevels: the fold levels are the whole session at a glance, its
// tabs, and everything.
func TestTreeFoldsToThreeLevels(t *testing.T) {
	_, m := treeFixture(t) // unfolded to the panes
	m = press(t, m, "h")

	if m.level != levelTabs {
		t.Fatalf("h from the panes folded to %v", m.level)
	}
	// The pane the cursor was on is folded away, so its tab stands in for it.
	tabs := []string{
		"  [1] w1S  herdr-arrange",
		" ▸ ├─ t1  main  4 panes  (current)  ← already in tab main",
		"   ├─ t2  logs  1 pane",
		"   └─ [c] new tab in this workspace",
		"  [2] wJ  notes",
		"   └─ t1  1 pane",
		"  [N] new workspace",
	}
	wantLines(t, m, tabs)

	// Folded again, only the workspaces and the row that makes another one are left.
	m = press(t, m, "h")
	if m.level != levelWorkspaces {
		t.Fatalf("h from the tabs folded to %v", m.level)
	}
	wantLines(t, m, []string{
		"▸ [1] w1S  herdr-arrange  ← move to a new tab",
		"  [2] wJ  notes",
		"  [N] new workspace",
	})

	// And there is nowhere further to fold, in either direction.
	if m = press(t, m, "h", "left"); m.level != levelWorkspaces {
		t.Errorf("folding past the workspaces reached %v", m.level)
	}
	m = press(t, m, "right")
	wantLines(t, m, tabs)
	if m = press(t, m, "l", "l", "right"); m.level != levelPanes {
		t.Errorf("unfolding past the panes reached %v", m.level)
	}
}

// wantLines checks the top of the rendered view line for line.
func wantLines(t *testing.T, m Model, want []string) {
	t.Helper()
	got := lines(m)
	for i, wantLine := range want {
		if i >= len(got) {
			t.Fatalf("the view has only %d lines, want at least %d", len(got), len(want))
		}
		if trimmed := strings.TrimRight(got[i], " "); trimmed != wantLine {
			t.Errorf("line %d:\n got %q\nwant %q", i+1, trimmed, wantLine)
		}
	}
}

// TestTreeFoldingKeepsThePopupsSize: the popup is built for the unfolded tree, so
// folding never has to close and reopen it — which is the only reason folding is free.
func TestTreeFoldingKeepsThePopupsSize(t *testing.T) {
	f := newFakeClient(evenFour())
	m := popupSized(t, f, ModeTree)

	for _, k := range []string{"h", "h", "l", "l"} {
		m = press(t, m, k)
		if m.reopen != nil || m.quitting {
			t.Fatalf("%q reopened the popup at %v", k, m.level)
		}
		if m.outgrewThePopup(treeSizeForRows(len(m.rows), m.room)) {
			t.Fatalf("at %v the tree thinks it has outgrown its popup", m.level)
		}
	}
}

// TestTreePreviewNamesEveryAction: with five kinds of row in one list, the preview
// is the only thing that makes enter predictable — and it has to be on the selected
// row, not merely somewhere on screen.
func TestTreePreviewNamesEveryAction(t *testing.T) {
	_, m := treeFixture(t)

	cases := []struct {
		what  string
		match func(row) bool
		want  string
	}{
		{"another workspace", func(r row) bool { return r.kind == rowWorkspace && r.workspaceID == "wJ" },
			"move to workspace notes"},
		{"the current workspace", func(r row) bool { return r.kind == rowWorkspace && r.workspaceID == curWS },
			"move to a new tab"},
		{"another tab", func(r row) bool { return r.kind == rowTab && r.tabID == "w1S:t2" },
			"move to tab logs"},
		{"the current tab", func(r row) bool { return r.kind == rowTab && r.tabID == curTab },
			"already in tab main"},
		{"a pane in this tab", func(r row) bool { return r.kind == rowPane && r.paneID == "w1S:p3" },
			"move next to p3, press s to swap"},
		{"a pane elsewhere", func(r row) bool { return r.kind == rowPane && r.paneID == "wJ:p1" },
			"move next to p1, press s to swap"},
		{"the arranged pane", func(r row) bool { return r.self },
			"back to layout mode"},
		{"new tab here", func(r row) bool { return r.kind == rowNewTabHere },
			"move to a new tab"},
		{"new workspace", func(r row) bool { return r.kind == rowNewWorkspace },
			"move to a new workspace"},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			m := selectRow(t, m, c.what, c.match)
			if got := m.treePreview(); got != c.want {
				t.Errorf("preview is %q, want %q", got, c.want)
			}

			line := selectedLine(m)
			if !strings.HasPrefix(strings.TrimLeft(line, " "), "▸ ") || !strings.Contains(line, "← "+c.want) {
				t.Errorf("the selected line reads %q, want %q beside the cursor", line, c.want)
			}
			// And nowhere else: one preview per view, on the row it is about.
			if n := strings.Count(plain(m.View()), "←"); n != 1 {
				t.Errorf("%d previews on screen:\n%s", n, plain(m.View()))
			}
		})
	}
}

// TestTreeEnterOnATabMovesThePaneAndArrangesItThere covers the main path through
// tree mode.
func TestTreeEnterOnATabMovesThePaneAndArrangesItThere(t *testing.T) {
	f, m := treeFixture(t)
	m = selectRow(t, m, "another tab", func(r row) bool { return r.tabID == "w1S:t2" && r.kind == rowTab })
	m = press(t, m, "enter")

	if !f.took("move w1S:p2 -> tab w1S:t2") {
		t.Fatalf("calls were: %s", f.log())
	}
	if m.mode != ModeLayout {
		t.Error("the popup did not switch to layout mode")
	}
	if m.quitting {
		t.Error("the popup closed instead of arranging the destination")
	}
	if m.status != "moved to t2" {
		t.Errorf("status is %q", m.status)
	}
}

// TestTreeEnterOnAWorkspaceMovesThePaneAndCloses: a workspace row creates a new
// tab there, so the completed move leaves nothing for the popup to arrange.
func TestTreeEnterOnAWorkspaceMovesThePaneAndCloses(t *testing.T) {
	f, m := treeFixture(t)
	m = selectRow(t, m, "another workspace", func(r row) bool {
		return r.kind == rowWorkspace && r.workspaceID == "wJ"
	})
	m = press(t, m, "enter")

	if !f.took("move w1S:p2 -> new_tab wJ") {
		t.Fatalf("calls were: %s", f.log())
	}
	if m.status != "moved to a new tab in notes" {
		t.Errorf("status is %q", m.status)
	}
	if !m.quitting {
		t.Fatal("the popup stayed open")
	}
}

// TestTreeEnterOnASameTabPaneMovesBesideIt: a pane row means the same thing wherever
// it is, so enter lands this pane beside the selected one in its own tab too — which
// takes a rebuild, because herdr refuses a move into the tab the pane is in.
func TestTreeEnterOnASameTabPaneMovesBesideIt(t *testing.T) {
	f, m := treeFixture(t)
	m = selectRow(t, m, "p3", func(r row) bool { return r.paneID == "w1S:p3" })
	m = press(t, m, "enter")

	// The last leg of the rebuild is the arranged pane landing beside p3.
	if !f.took("move w1S:p2 -> tab w1S:t1 w1S:p3") {
		t.Fatalf("calls were: %s", f.log())
	}
	if m.mode != ModeLayout {
		t.Error("the popup did not switch to layout mode")
	}
	if m.status != "moved next to p3" {
		t.Errorf("status is %q", m.status)
	}
}

// TestTreeSwapTradesPlacesAndCloses: `s` is the cheap alternative to landing beside a
// pane — the tab keeps its shape — and a swap is the whole request, so there is
// nothing left to do afterwards.
func TestTreeSwapTradesPlacesAndCloses(t *testing.T) {
	f, m := treeFixture(t)
	m = selectRow(t, m, "p3", func(r row) bool { return r.paneID == "w1S:p3" })
	m = press(t, m, "s")

	if !f.took("swap w1S:p2 w1S:p3") {
		t.Fatalf("calls were: %s", f.log())
	}
	if f.took("move") {
		t.Errorf("the swap rebuilt the tab: %s", f.log())
	}
	if !m.quitting {
		t.Error("the popup stayed open after the swap")
	}
}

// TestTreeSwapCrossesTabs: a pane elsewhere is someone to trade places with, not
// only somewhere to land, so `s` reaches it too — through the engine's stand-in
// dance, since herdr's own pane.swap stops at the tab boundary.
func TestTreeSwapCrossesTabs(t *testing.T) {
	f, m := treeFixture(t)
	m = selectRow(t, m, "wJ:p1", func(r row) bool { return r.paneID == "wJ:p1" })
	m = press(t, m, "s")

	// The stand-in holds this pane's slot, the two panes cross, and the stand-in goes.
	for _, want := range []string{
		"split w1S:p2 right",
		"move w1S:p2 -> tab wJ:t1 wJ:p1",
		"move wJ:p1 -> tab w1S:t1 w1S:p8",
		"close w1S:p8",
	} {
		if !f.took(want) {
			t.Fatalf("no %q; calls were: %s", want, f.log())
		}
	}
	if !m.quitting {
		t.Error("the popup stayed open after the swap")
	}
}

// TestTreeSwapOnATabTakesItsFirstPane: the tree opens folded to the tabs, so `s`
// has to mean something there — and what it means is the row unfolding would show
// first, which is the same pane enter's "move to tab" would land beside.
func TestTreeSwapOnATabTakesItsFirstPane(t *testing.T) {
	f, m := treeFixture(t)
	m = selectRow(t, m, "another tab", func(r row) bool { return r.kind == rowTab && r.tabID == "w1S:t2" })
	m = press(t, m, "s")

	// w1S:t2 holds one pane, w1S:p7, and both panes are in this workspace's tabs —
	// so this is the cross-tab exchange, not a pane.swap.
	if !f.took("move w1S:p7 -> tab w1S:t1") {
		t.Fatalf("the tab's first pane did not come back; calls were: %s", f.log())
	}
	if !f.took("move w1S:p2 -> tab w1S:t2 w1S:p7") {
		t.Fatalf("the arranged pane did not go beside p7; calls were: %s", f.log())
	}
	if !m.quitting {
		t.Error("the popup stayed open after the swap")
	}
}

// TestTreeSwapSaysWhenItCannot: a workspace row and the arranged pane are the two
// things there is nothing to trade places with, so `s` says so rather than moving
// the pane somewhere the user did not ask for.
func TestTreeSwapSaysWhenItCannot(t *testing.T) {
	cases := []struct {
		what  string
		match func(row) bool
		want  string
	}{
		{"a workspace", func(r row) bool { return r.kind == rowWorkspace && r.workspaceID == "wJ" },
			"select a pane or a tab to swap with"},
		{"new tab here", func(r row) bool { return r.kind == rowNewTabHere },
			"select a pane or a tab to swap with"},
		{"the arranged pane", func(r row) bool { return r.self },
			"select another pane to swap with"},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			f, m := treeFixture(t)
			m = selectRow(t, m, c.what, c.match)
			m = press(t, m, "s")

			if m.statusKind != statusFlash || m.status != c.want {
				t.Errorf("status is %v %q, want a flash of %q", m.statusKind, m.status, c.want)
			}
			if len(f.calls) != 0 {
				t.Errorf("the session was touched: %s", f.log())
			}
			if m.quitting || m.mode != ModeTree {
				t.Error("the popup left tree mode")
			}
		})
	}
}

// TestTreeEnterOnAPaneElsewhereMovesBesideIt: a pane in another tab is a place to
// land, in one pane.move — and never a swap, which cannot cross a tab boundary.
func TestTreeEnterOnAPaneElsewhereMovesBesideIt(t *testing.T) {
	f, m := treeFixture(t)
	m = selectRow(t, m, "wJ:p1", func(r row) bool { return r.paneID == "wJ:p1" })
	m = press(t, m, "enter")

	if !f.took("move w1S:p2 -> tab wJ:t1 wJ:p1") {
		t.Fatalf("calls were: %s", f.log())
	}
	if f.took("swap") {
		t.Errorf("a cross-tab swap was attempted: %s", f.log())
	}
	if m.mode != ModeLayout {
		t.Error("the popup did not switch to layout mode")
	}
}

// TestTreeEnterOnItsOwnTabFlashes: selecting the tab the pane is already in is a
// reasonable mistake, not an error.
func TestTreeEnterOnItsOwnTabFlashes(t *testing.T) {
	f, m := treeFixture(t)
	m = selectRow(t, m, "the current tab", func(r row) bool { return r.kind == rowTab && r.tabID == curTab })
	m = press(t, m, "enter")

	if m.statusKind != statusFlash || m.status != "nothing to change" {
		t.Fatalf("status is %v %q", m.statusKind, m.status)
	}
	if f.took("move") {
		t.Errorf("the pane was moved anyway: %s", f.log())
	}
}

// TestTreeEnterOnTheArrangedPaneGoesBackToLayout is the way back for a popup that
// opened straight into tree mode.
func TestTreeEnterOnTheArrangedPaneGoesBackToLayout(t *testing.T) {
	f, m := treeFixture(t)
	m = press(t, m, "enter")

	if m.mode != ModeLayout {
		t.Fatal("the popup did not switch to layout mode")
	}
	if m.quitting {
		t.Error("the popup closed")
	}
	if f.took("move") || f.took("swap") {
		t.Errorf("switching views touched the session: %s", f.log())
	}
}

// TestTreeSinglePaneTabHasNoLayoutToShow: with one pane there is nothing to
// arrange, so both routes into layout mode say so instead of opening an empty view.
func TestTreeSinglePaneTabHasNoLayoutToShow(t *testing.T) {
	for _, k := range []string{"enter", "t"} {
		t.Run(k, func(t *testing.T) {
			f := newFakeClient(tree.Leaf(curPane))
			// Unfolded, so the cursor is on the pane: enter is only that route in
			// when the pane's own row is on screen.
			m := press(t, start(t, f, ModeTree), "l", k)

			if m.mode != ModeTree {
				t.Error("the popup left tree mode")
			}
			if m.status != "this tab has only one pane" {
				t.Errorf("status is %q", m.status)
			}
			if got := m.treePreview(); got != "the pane you are moving" {
				t.Errorf("preview is %q", got)
			}
		})
	}
}

// TestTreeShortcutsActWithoutNavigating: the point of [c], [N] and 1-9 is to skip
// the walk down the tree.
func TestTreeShortcutsActWithoutNavigating(t *testing.T) {
	cases := []struct {
		key      string
		want     string
		status   string
		quitting bool
	}{
		{"c", "move w1S:p2 -> new_tab", "moved to a new tab", true},
		{"N", "move w1S:p2 -> new_workspace", "moved to a new workspace", true},
		{"1", "move w1S:p2 -> new_tab w1S", "moved to a new tab in herdr-arrange", true},
		{"2", "move w1S:p2 -> new_tab wJ", "moved to a new tab in notes", true},
	}

	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			f, m := treeFixture(t)
			m = press(t, m, c.key)

			if !f.took(c.want) {
				t.Fatalf("calls were: %s", f.log())
			}
			if m.status != c.status {
				t.Errorf("status is %q, want %q", m.status, c.status)
			}
			if m.quitting != c.quitting {
				t.Errorf("quitting=%v, want %v", m.quitting, c.quitting)
			}
			if !c.quitting && m.mode != ModeLayout {
				t.Error("the popup did not switch to layout mode")
			}
		})
	}

	// A workspace that is not there is not an error either.
	f, m := treeFixture(t)
	if m = press(t, m, "9"); len(f.calls) != 0 {
		t.Errorf("9 acted on a workspace that does not exist: %s", f.log())
	}
}

// TestTreeScrollsToKeepTheCursorVisible: the popup is shorter than most sessions'
// trees, so the selection has to drag the viewport along.
func TestTreeScrollsToKeepTheCursorVisible(t *testing.T) {
	f := newFakeClient(evenFour())
	m := send(t, start(t, f, ModeTree), sizeMsg(66, 9), key(t, "l")) // room for five rows

	if m.vp.Height != 5 {
		t.Fatalf("viewport height is %d, want 5", m.vp.Height)
	}

	visible := func(m Model) bool {
		return m.cursorLine() >= m.vp.YOffset && m.cursorLine() < m.vp.YOffset+m.vp.Height
	}
	if !visible(m) {
		t.Fatalf("the initial cursor at %d is outside rows %d..%d", m.cursorLine(), m.vp.YOffset, m.vp.YOffset+m.vp.Height)
	}

	// Walk the whole tree in both directions; the cursor must never leave the view.
	for _, k := range []string{"G", "g", "pgdown", "pgup", "ctrl+d", "ctrl+u", "end", "home"} {
		m = press(t, m, k)
		if !visible(m) {
			t.Fatalf("after %q the cursor at %d is outside rows %d..%d",
				k, m.cursorLine(), m.vp.YOffset, m.vp.YOffset+m.vp.Height)
		}
	}
	for range len(m.rows) + 3 {
		m = press(t, m, "j")
		if !visible(m) {
			t.Fatalf("scrolling down left the cursor at %d outside rows %d..%d",
				m.cursorLine(), m.vp.YOffset, m.vp.YOffset+m.vp.Height)
		}
	}
	if m.cursor != len(m.rows)-1 {
		t.Errorf("j past the end left the cursor at %d of %d rows", m.cursor, len(m.rows))
	}
	for range len(m.rows) + 3 {
		m = press(t, m, "k")
	}
	if m.cursor != 0 {
		t.Errorf("k past the start left the cursor at %d", m.cursor)
	}
}

// TestTreeSelectionSurvivesAReload keeps a failed action from throwing the user
// back to the top of the tree.
func TestTreeSelectionSurvivesAReload(t *testing.T) {
	_, m := treeFixture(t)
	m = selectRow(t, m, "the current tab", func(r row) bool { return r.kind == rowTab && r.tabID == curTab })
	was := m.cursor

	// Moving into its own tab is refused, and the refusal reloads the tree.
	m = press(t, m, "enter")
	if m.cursor != was {
		t.Errorf("the cursor moved from %d to %d", was, m.cursor)
	}
}

// TestTreeSelectedRowIsBold: the ▸ is one thin mark at the left edge of a list of
// near-identical rows, so the row's own words carry the selection too.
//
// The rest of the suite strips styling, and the profile the tests run under has none
// to strip; this one turns colour on to see the attribute at all.
func TestTreeSelectedRowIsBold(t *testing.T) {
	was := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(was)

	_, m := treeFixture(t)
	r := m.rows[rowAt(t, m, "another tab", func(r row) bool { return r.kind == rowTab && r.tabID == "w1S:t2" })]

	bold := "\x1b[1m" + r.label
	if got := m.renderRow(r, true); !strings.Contains(got, bold) {
		t.Errorf("the selected row renders as %q, want %q bold", got, r.label)
	}
	if got := m.renderRow(r, false); strings.Contains(got, bold) {
		t.Errorf("an unselected row renders as %q, want its label unbolded", got)
	}
}

// TestTreePreviewNeverWrapsARow: the preview shares a line with the row it is about,
// and a line that wraps costs the tree a row at the bottom of the popup.
func TestTreePreviewNeverWrapsARow(t *testing.T) {
	for _, width := range []int{18, 30, 45, 63} {
		f := newFakeClient(evenFour())
		m := send(t, start(t, f, ModeTree), sizeMsg(width, 20), key(t, "l"))

		for i, line := range lines(m) {
			if got := len([]rune(strings.TrimRight(line, " "))); got > width {
				t.Errorf("at width %d line %d is %d columns: %q", width, i+1, got, line)
			}
		}
		// Narrow enough and there is nothing worth saying; wide enough and it is said.
		selected := selectedLine(m)
		if want := width >= 45; strings.Contains(selected, "←") != want {
			t.Errorf("at width %d the selected row reads %q", width, selected)
		}
	}
}

// TestTreeViewFitsThePopup keeps the tree from pushing its own help off screen.
func TestTreeViewFitsThePopup(t *testing.T) {
	for _, height := range []int{6, 9, 15, 30} {
		f := newFakeClient(evenFour())
		m := send(t, start(t, f, ModeTree), sizeMsg(57, height))

		got := lines(m)
		if len(got) != height {
			t.Errorf("at height %d the view is %d lines:\n%s", height, len(got), plain(m.View()))
		}
		for i, line := range got {
			if width := len([]rune(strings.TrimRight(line, " "))); width > 57 {
				t.Errorf("at height %d line %d is %d columns: %q", height, i+1, width, line)
			}
		}
	}
}
