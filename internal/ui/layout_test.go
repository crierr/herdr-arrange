package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/crierr/herdr-arrange/internal/engine"
	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

// TestLayoutKeysReachTheRightOperation checks the whole keymap against the calls
// each key is supposed to make. It is the only place the letter form and the
// arrow form of a binding are proved to do the same thing.
func TestLayoutKeysReachTheRightOperation(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		// A directional swap is one call, with no tree maths behind it.
		{"h", "swap-dir w1S:p2 left"},
		{"left", "swap-dir w1S:p2 left"},
		{"j", "swap-dir w1S:p2 down"},
		{"down", "swap-dir w1S:p2 down"},
		{"k", "swap-dir w1S:p2 up"},
		{"up", "swap-dir w1S:p2 up"},
		{"l", "swap-dir w1S:p2 right"},
		{"right", "swap-dir w1S:p2 right"},

		// Re-splitting left within [[p1 | p2] | [p3 | p4]] only reorders leaves,
		// so the reconciler gets there with an explicit swap rather than a rebuild.
		{"H", "swap w1S:p1 w1S:p2"},
		{"shift+left", "swap w1S:p1 w1S:p2"},

		// Re-splitting up changes a split's direction, which needs a rebuild: the
		// first pane out of the tab creates the parking tab.
		{"K", "move w1S:p1 -> new_tab"},
		{"shift+up", "move w1S:p1 -> new_tab"},

		// even-vertical is a different shape, so it rebuilds too — and here the
		// arranged pane is not the anchor, so it is parked and reinserted itself.
		{"2", "move w1S:p2 -> tab w1S:t1"},

		{"c", "move w1S:p2 -> new_tab"},
		{"N", "move w1S:p2 -> new_workspace"},
	}

	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			f := newFakeClient(evenFour())
			m := press(t, start(t, f, ModeLayout), c.key)

			if !f.took(c.want) {
				t.Fatalf("%q did not %q; calls were: %s", c.key, c.want, f.log())
			}
			if m.statusKind == statusFail {
				t.Errorf("%q reported a failure: %s", c.key, m.status)
			}
			if m.quitting {
				t.Errorf("%q closed the popup", c.key)
			}
		})
	}
}

// TestRebuildEndsWithFocusBackOnThePane guards the thing a user would notice
// immediately: every pane.move is made unfocused, so something has to put focus
// back afterwards.
func TestRebuildEndsWithFocusBackOnThePane(t *testing.T) {
	f := newFakeClient(evenFour())
	press(t, start(t, f, ModeLayout), "2")

	if last := f.calls[len(f.calls)-1]; !strings.HasPrefix(last, "export") {
		t.Fatalf("the last call is %q, want the reload", last)
	}
	if !f.took("focus w1S:p2") {
		t.Fatalf("focus was never restored; calls were: %s", f.log())
	}
}

// TestPresetAlreadyAppliedFlashes covers the common case of pressing the number
// for the layout the tab is already in.
func TestPresetAlreadyAppliedFlashes(t *testing.T) {
	f := newFakeClient(evenFour()) // this is what even-horizontal builds
	m := press(t, start(t, f, ModeLayout), "1")

	if m.statusKind != statusFlash {
		t.Fatalf("status kind is %v (%q), want a flash", m.statusKind, m.status)
	}
	if m.status != "nothing to change" {
		t.Errorf("status is %q", m.status)
	}
	if f.took("move") || f.took("swap") {
		t.Errorf("the tab was touched anyway: %s", f.log())
	}
}

// TestEqualizeOnlySetsRatios is the point of having a separate key for it: it
// must never move a pane, because moving panes is what flickers.
func TestEqualizeOnlySetsRatios(t *testing.T) {
	lopsided := tree.Split(herdr.Right, 0.8,
		tree.Split(herdr.Right, 0.5, tree.Leaf("w1S:p1"), tree.Leaf(curPane)),
		tree.Split(herdr.Right, 0.5, tree.Leaf("w1S:p3"), tree.Leaf("w1S:p4")),
	)
	f := newFakeClient(lopsided)
	m := press(t, start(t, f, ModeLayout), "e")

	if !f.took("ratio w1S:t1 [] = 0.50") {
		t.Fatalf("the root ratio was not evened out; calls were: %s", f.log())
	}
	if f.took("move") || f.took("swap") {
		t.Fatalf("equalize moved a pane: %s", f.log())
	}
	if m.status != "sizes evened out" {
		t.Errorf("status is %q", m.status)
	}
}

// TestSwapWithNoNeighbourFlashes checks that herdr declining to swap reads as
// "there is nothing that way", not as an error.
func TestSwapWithNoNeighbourFlashes(t *testing.T) {
	f := newFakeClient(evenFour())
	f.swapRefused = true
	m := press(t, start(t, f, ModeLayout), "h")

	if m.statusKind != statusFlash {
		t.Fatalf("status kind is %v (%q), want a flash", m.statusKind, m.status)
	}
	if m.status != "no pane to the left" {
		t.Errorf("status is %q", m.status)
	}
}

// TestSinglePaneTabRefusesToRearrange keeps the keys that need a neighbour from
// silently doing nothing.
func TestSinglePaneTabRefusesToRearrange(t *testing.T) {
	for _, k := range []string{"h", "H", "1", "5", " ", "e"} {
		t.Run(k, func(t *testing.T) {
			f := newFakeClient(tree.Leaf(curPane))
			m := press(t, start(t, f, ModeLayout), k)

			if m.status != "this tab has only one pane" {
				t.Errorf("status is %q", m.status)
			}
			if len(f.calls) != 0 {
				t.Errorf("herdr was called anyway: %s", f.log())
			}
		})
	}
}

// TestSinglePaneTabStillMovesThePane checks the keys that do make sense with one
// pane are not caught by the same guard.
func TestSinglePaneTabStillMovesThePane(t *testing.T) {
	f := newFakeClient(tree.Leaf(curPane))
	press(t, start(t, f, ModeLayout), "N")

	if !f.took("move w1S:p2 -> new_workspace") {
		t.Fatalf("calls were: %s", f.log())
	}
}

// TestMovingToANewWorkspaceRetargetsThePane covers herdr's workspace-scoped ids:
// the pane is renamed by the move, and the popup has to follow it.
func TestMovingToANewWorkspaceRetargetsThePane(t *testing.T) {
	f := newFakeClient(evenFour())
	m := press(t, start(t, f, ModeLayout), "N")

	if got := m.eng.PaneID(); got != "w2A:p1" {
		t.Fatalf("the popup still points at %s", got)
	}
	if state := m.layoutState(); !strings.Contains(state, "w2A:t1") || !strings.Contains(state, "this pane: p1") {
		t.Errorf("state line is %q", state)
	}
}

// TestLayoutViewShowsWhereWeAre checks the status line, which is the only thing
// telling the user what `space` will cycle from.
func TestLayoutViewShowsWhereWeAre(t *testing.T) {
	f := newFakeClient(evenFour())
	view := plain(start(t, f, ModeLayout).View())

	for _, want := range []string{
		"h/j/k/l", "shift+←↓↑→", "even-horizontal", "tiled", "cycle presets",
		"move pane to a new workspace", "enter / esc",
		"w1S:t1 · 4 panes · even-horizontal · this pane: p2",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the view is missing %q:\n%s", want, view)
		}
	}
}

// TestLayoutPanelMatchesTheRequestedPopupSize pins the help panel to the size the
// action asks herdr for. If the panel grows, this fails rather than the popup
// quietly clipping it.
func TestLayoutPanelMatchesTheRequestedPopupSize(t *testing.T) {
	f := newFakeClient(evenFour())
	// Somewhere far larger than the panel, so nothing is clipped.
	m := send(t, start(t, f, ModeLayout), sizeMsg(200, 60))

	got := lines(m)
	if len(got) != layoutPanelHeight {
		t.Errorf("the panel is %d lines, but the action asks for %d:\n%s",
			len(got), layoutPanelHeight, plain(m.View()))
	}
	for i, line := range got {
		trimmed := strings.TrimRight(line, " ")
		if strings.Trim(trimmed, "─") == "" {
			continue // the rule is meant to span whatever width it is given
		}
		if width := len([]rune(trimmed)); width > layoutPanelWidth {
			t.Errorf("line %d is %d columns, but the action asks for %d: %q",
				i+1, width, layoutPanelWidth, line)
		}
	}
}

// TestLayoutViewSurvivesASmallPopup: herdr clamps an oversized popup down to the
// terminal, so the panel has to cope with less room than it asked for. Help gives
// way; the status lines are what the user needs to still be there.
func TestLayoutViewSurvivesASmallPopup(t *testing.T) {
	f := newFakeClient(evenFour())
	m := start(t, f, ModeLayout)

	for _, size := range [][2]int{{63, 12}, {40, 6}, {20, 4}, {12, 3}} {
		width, height := size[0], size[1]
		m = send(t, m, sizeMsg(width, height))

		got := lines(m)
		if len(got) > height {
			t.Errorf("at %dx%d the view is %d lines:\n%s", width, height, len(got), plain(m.View()))
		}
		for i, line := range got {
			if w := len([]rune(strings.TrimRight(line, " "))); w > width {
				t.Errorf("at %dx%d line %d is %d columns: %q", width, height, i+1, w, line)
			}
		}
	}

	// At the size the action actually asks for, nothing is lost.
	view := plain(send(t, m, sizeMsg(63, 12)).View())
	if !strings.Contains(view, "this pane: p2") || !strings.Contains(view, "swap pane") {
		t.Errorf("the full-size panel is missing something:\n%s", view)
	}
}

// TestLayoutNameIsRelativeToTheArrangedPane: [p1 | [p2 / p3]] is main-vertical
// for p1, but calling it that while p2 is the pane being arranged would promise
// something `4` would immediately change.
func TestLayoutNameIsRelativeToTheArrangedPane(t *testing.T) {
	mainVerticalForP1 := tree.Split(herdr.Right, 0.5,
		tree.Leaf("w1S:p1"),
		tree.Split(herdr.Down, 0.5, tree.Leaf(curPane), tree.Leaf("w1S:p3")),
	)
	f := newFakeClient(mainVerticalForP1)
	m := start(t, f, ModeLayout)

	if !strings.Contains(m.layoutState(), "custom") {
		t.Errorf("state line is %q, want it to read custom", m.layoutState())
	}

	// The same tree with the arranged pane in the main slot is the real thing.
	f = newFakeClient(tree.Split(herdr.Right, 0.5,
		tree.Leaf(curPane),
		tree.Split(herdr.Down, 0.5, tree.Leaf("w1S:p1"), tree.Leaf("w1S:p3")),
	))
	if state := start(t, f, ModeLayout).layoutState(); !strings.Contains(state, "main-vertical") {
		t.Errorf("state line is %q, want main-vertical", state)
	}
}

// TestFailedOperationStaysOpen: a transport failure is worth reading, so the
// popup reports it and leaves the user in charge.
func TestFailedOperationStaysOpen(t *testing.T) {
	f := newFakeClient(evenFour())
	f.opErr = errors.New("socket went away")
	m := press(t, start(t, f, ModeLayout), "h")

	if m.quitting {
		t.Fatal("the popup closed on a recoverable error")
	}
	if m.statusKind != statusFail || !strings.Contains(m.status, "socket went away") {
		t.Fatalf("status is %v %q", m.statusKind, m.status)
	}
	if !strings.Contains(plain(m.View()), "socket went away") {
		t.Error("the failure is not on screen")
	}
}

// TestVanishedPaneClosesThePopup: there is nothing left to arrange, and guessing
// at another pane would be worse than closing.
func TestVanishedPaneClosesThePopup(t *testing.T) {
	f := newFakeClient(evenFour())
	m := start(t, f, ModeLayout)
	f.exportErr = &herdr.APIError{Code: "not_found", Message: "no such pane"}
	m = press(t, m, "h")

	if !m.quitting {
		t.Fatal("the popup stayed open")
	}
	if !errors.Is(m.fatal, engine.ErrPaneGone) {
		t.Fatalf("fatal error is %v", m.fatal)
	}
	if m.View() != "" {
		t.Errorf("a closing popup still drew something: %q", m.View())
	}
}

// TestClosingKeys covers every way out.
func TestClosingKeys(t *testing.T) {
	for _, k := range []string{"enter", "esc", "q", "ctrl+c"} {
		t.Run(k, func(t *testing.T) {
			f := newFakeClient(evenFour())
			if m := press(t, start(t, f, ModeLayout), k); !m.quitting {
				t.Errorf("%q did not close the popup", k)
			}
		})
	}
}

// TestBusyIgnoresKeys stops a second keypress from starting a rebuild on top of
// one already running, which would interleave two plans over the same tab.
func TestBusyIgnoresKeys(t *testing.T) {
	f := newFakeClient(evenFour())
	m := start(t, f, ModeLayout)
	m.busy = true

	m = press(t, m, "2")
	if len(f.calls) != 0 {
		t.Fatalf("a key was acted on while busy: %s", f.log())
	}
	if !strings.Contains(plain(m.View()), "working…") {
		t.Error("the popup does not say it is working")
	}

	// ctrl+c is the exception: it must always get the user out.
	if m = press(t, m, "ctrl+c"); !m.quitting {
		t.Error("ctrl+c did not close a busy popup")
	}
}

// TestCyclePresetNamesWhatItApplied: `space` is only predictable if the popup says
// where it landed.
func TestCyclePresetNamesWhatItApplied(t *testing.T) {
	f := newFakeClient(evenFour())
	m := press(t, start(t, f, ModeLayout), " ")

	if !strings.HasPrefix(m.status, "applied ") || m.status == "applied even-horizontal" {
		t.Fatalf("status is %q, want a different preset", m.status)
	}
	if m.statusKind != statusInfo {
		t.Errorf("status kind is %v", m.statusKind)
	}
}
