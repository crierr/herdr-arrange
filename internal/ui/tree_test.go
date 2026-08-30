package ui

import (
	"strings"
	"testing"

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
		"   ├─ t1  main  4 panes",
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
		" ▸ ├─ t1  main  4 panes  ← this pane is already in t1",
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
		"▸ [1] w1S  herdr-arrange  ← new tab here",
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
			"new tab here"},
		{"the current workspace", func(r row) bool { return r.kind == rowWorkspace && r.workspaceID == curWS },
			"new tab here"},
		{"another tab", func(r row) bool { return r.kind == rowTab && r.tabID == "w1S:t2" },
			"move this pane to t2"},
		{"the current tab", func(r row) bool { return r.kind == rowTab && r.tabID == curTab },
			"this pane is already in t1"},
		{"a pane in this tab", func(r row) bool { return r.kind == rowPane && r.paneID == "w1S:p3" },
			"swap this pane with p3"},
		{"a pane elsewhere", func(r row) bool { return r.kind == rowPane && r.paneID == "wJ:p1" },
			"move this pane next to p1"},
		{"the arranged pane", func(r row) bool { return r.self },
			"back to layout mode"},
		{"new tab here", func(r row) bool { return r.kind == rowNewTabHere },
			"new tab here"},
		{"new workspace", func(r row) bool { return r.kind == rowNewWorkspace },
			"move this pane to a new workspace"},
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

// TestTreeEnterOnASameTabPaneSwapsAndCloses: a swap is the whole request, so
// there is nothing left to do afterwards.
func TestTreeEnterOnASameTabPaneSwapsAndCloses(t *testing.T) {
	f, m := treeFixture(t)
	m = selectRow(t, m, "p3", func(r row) bool { return r.paneID == "w1S:p3" })
	m = press(t, m, "enter")

	if !f.took("swap w1S:p2 w1S:p3") {
		t.Fatalf("calls were: %s", f.log())
	}
	if !m.quitting {
		t.Error("the popup stayed open after the swap")
	}
}

// TestTreeEnterOnAPaneElsewhereMovesBesideIt: pane.swap cannot cross tabs, so a
// pane in another tab is offered as a place to land rather than as a swap.
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
		key    string
		want   string
		status string
	}{
		{"c", "move w1S:p2 -> new_tab", "moved to a new tab"},
		{"N", "move w1S:p2 -> new_workspace", "moved to a new workspace"},
		{"1", "move w1S:p2 -> new_tab w1S", "moved to a new tab in herdr-arrange"},
		{"2", "move w1S:p2 -> new_tab wJ", "moved to a new tab in notes"},
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
			if m.mode != ModeLayout {
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
