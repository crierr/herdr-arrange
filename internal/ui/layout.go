package ui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

// The size layout mode wants. The action asks herdr for a popup this big, so the
// help panel and the popup geometry cannot drift apart; the tests hold the panel
// to these numbers.
//
// The height is the map (minimapHeight), ten lines of help and the two status lines
// under their rule (statusHeight).
const (
	layoutPanelWidth  = 60
	layoutPanelHeight = minimapHeight + 10 + statusHeight
)

// layoutKey handles a keypress in layout mode.
func (m Model) layoutKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Everything that rearranges the tab needs someone to rearrange it with.
	if dir, ok := swapKeys[key]; ok {
		if !m.multiPane() {
			return m.flashSinglePane()
		}
		eng := m.eng
		return m, m.op(nextStay, func(ctx context.Context) (string, error) {
			return "swapped with the pane " + dirWord(dir), eng.Swap(ctx, dir)
		})
	}
	if dir, ok := reSplitKeys[key]; ok {
		if !m.multiPane() {
			return m.flashSinglePane()
		}
		eng := m.eng
		return m, m.op(nextStay, func(ctx context.Context) (string, error) {
			return "re-split to the " + sideWord(dir), eng.ReSplit(ctx, dir)
		})
	}
	if dir, ok := resizeKeys[key]; ok {
		if !m.multiPane() {
			return m.flashSinglePane()
		}
		eng := m.eng
		return m, m.op(nextStay, func(ctx context.Context) (string, error) {
			return "the split moved " + string(dir), eng.Resize(ctx, dir)
		})
	}
	if n, err := strconv.Atoi(key); err == nil && n >= 1 && n <= len(tree.Presets) {
		if !m.multiPane() {
			return m.flashSinglePane()
		}
		eng, preset := m.eng, tree.Presets[n-1]
		return m, m.op(nextStay, func(ctx context.Context) (string, error) {
			return "applied " + preset.Name(), eng.ApplyPreset(ctx, preset)
		})
	}

	switch key {
	case " ":
		if !m.multiPane() {
			return m.flashSinglePane()
		}
		eng := m.eng
		return m, m.op(nextStay, func(ctx context.Context) (string, error) {
			preset, err := eng.CyclePreset(ctx)
			return "applied " + preset.Name(), err
		})

	case "e":
		if !m.multiPane() {
			return m.flashSinglePane()
		}
		eng := m.eng
		return m, m.op(nextStay, func(ctx context.Context) (string, error) {
			exact, err := eng.Balance(ctx)
			switch {
			case errors.Is(err, tree.ErrNoChange):
				return "", fmt.Errorf("the pane sizes are already even: %w", tree.ErrNoChange)
			case !exact:
				// Being honest beats claiming an evenness herdr's ratio clamp
				// will not give us.
				return "evened out as far as herdr's ratio limits allow", err
			}
			return "pane sizes evened out", err
		})

	case "c":
		eng := m.eng
		return m, m.op(nextStay, func(ctx context.Context) (string, error) {
			return "moved to a new tab", eng.MoveToNewTab(ctx, "")
		})

	case "N":
		eng := m.eng
		return m, m.op(nextStay, func(ctx context.Context) (string, error) {
			return "moved to a new workspace", eng.MoveToNewWorkspace(ctx)
		})

	case "t":
		m.status, m.statusKind = "", statusNone
		return m.switchTo(ModeTree)

	case "enter":
		m.quitting = true
		return m, tea.Quit
	}

	return m, nil
}

// swapKeys and reSplitKeys are the two direction families. herdr's popup gets
// every key ahead of its own bindings, including shift+arrows, so both the letter
// and the arrow form are available.
var swapKeys = map[string]herdr.Direction{
	"h": herdr.Left, "left": herdr.Left,
	"j": herdr.DirDn, "down": herdr.DirDn,
	"k": herdr.Up, "up": herdr.Up,
	"l": herdr.DirRt, "right": herdr.DirRt,
}

var reSplitKeys = map[string]herdr.Direction{
	"H": herdr.Left, "shift+left": herdr.Left,
	"J": herdr.DirDn, "shift+down": herdr.DirDn,
	"K": herdr.Up, "shift+up": herdr.Up,
	"L": herdr.DirRt, "shift+right": herdr.DirRt,
}

// resizeKeys move a boundary rather than a pane: the split nearest the pane in that
// direction goes that way, as tmux's resize-pane does. So ctrl+l widens a pane with
// something to its right and narrows one with something to its left, and either way
// the line you were pointing at is the line that moves.
var resizeKeys = map[string]herdr.Direction{
	"ctrl+h": herdr.Left, "ctrl+left": herdr.Left,
	"ctrl+j": herdr.DirDn, "ctrl+down": herdr.DirDn,
	"ctrl+k": herdr.Up, "ctrl+up": herdr.Up,
	"ctrl+l": herdr.DirRt, "ctrl+right": herdr.DirRt,
}

// dirWord names where a neighbour is, for the swap status line.
func dirWord(dir herdr.Direction) string {
	switch dir {
	case herdr.Left:
		return "to the left"
	case herdr.DirRt:
		return "to the right"
	case herdr.Up:
		return "above"
	default:
		return "below"
	}
}

// sideWord names the edge a re-split moves the pane to. Re-splitting is about a
// side of a region, not about a neighbour, so it wants different words.
func sideWord(dir herdr.Direction) string {
	switch dir {
	case herdr.Left:
		return "left"
	case herdr.DirRt:
		return "right"
	case herdr.Up:
		return "top"
	default:
		return "bottom"
	}
}

// multiPane reports whether the tab has anything to rearrange.
func (m Model) multiPane() bool { return m.tab != nil && m.tab.PaneCount() > 1 }

func (m Model) flashSinglePane() (tea.Model, tea.Cmd) {
	return m.flash("this tab has only one pane")
}

// layoutView renders layout mode: a static help panel over the two status lines.
func (m Model) layoutView() string {
	t := m.theme
	on := m.multiPane()

	// help renders one "keys  description" line. The direction bindings are wide
	// enough to need their own column — 25 is the resize line plus a gap; the single
	// keys below the presets get a narrower one, so neither block is padded out to the
	// other's width.
	help := func(keys, desc string, width int, enabled bool) string {
		return " " + pad(t.keyOr(enabled).Render(keys), width) + t.descOr(enabled).Render(desc)
	}

	lines := []string{
		help("h/j/k/l  ←↓↑→", "swap pane", 25, on),
		help("H/J/K/L  shift+←↓↑→", "re-split pane", 25, on),
		help("ctrl+h/j/k/l  ctrl+←↓↑→", "resize pane", 25, on),
	}

	// The presets go in a grid, with the one the tab currently matches picked out
	// so `space` is predictable.
	cells := make([]string, 0, len(tree.Presets)+1)
	for i, preset := range tree.Presets {
		name := t.descOr(on).Render(preset.Name())
		if on && m.tab.HasPreset && m.tab.Preset == preset {
			name = t.active.Render(preset.Name())
		}
		cells = append(cells, t.keyOr(on).Render(strconv.Itoa(i+1))+" "+name)
	}
	cells = append(cells, t.keyOr(on).Render("space")+" "+t.descOr(on).Render("cycle presets"))
	for i := 0; i < len(cells); i += 3 {
		row := " "
		for _, cell := range cells[i:min(i+3, len(cells))] {
			row += pad(cell, 20)
		}
		lines = append(lines, strings.TrimRight(row, " "))
	}

	lines = append(lines,
		help("e", "even out pane sizes", 14, on),
		help("c", "move pane to a new tab in this workspace", 14, true),
		help("N", "move pane to a new workspace", 14, true),
		help("t", "move/swap to another workspace/tab", 14, true),
		help("enter / esc", "close", 14, true),
	)

	room := m.height - statusHeight

	// The map goes above the help, and is the first thing to go when the popup is
	// shorter than the panel asked for: it is a picture of what the help acts on, so
	// it is worth less than the help itself. All of it or none of it — half a map
	// would be a lie about where the panes are.
	if room >= len(lines)+minimapHeight {
		lines = append(m.minimapBlock(), lines...)
	}

	// In a popup too short even for the help, help is what gives way: the status
	// lines are the only thing telling the user what they are looking at.
	if room < len(lines) {
		lines = lines[:max(room, 0)]
	}
	return strings.Join(append(lines, m.statusLines(m.layoutState())), "\n")
}

// layoutState is the "where am I" line: "w1S:t1 · 4 panes · main-vertical · this
// pane: p2".
func (m Model) layoutState() string {
	if m.tab == nil {
		return "reading the current tab…"
	}
	panes := "1 pane"
	if n := m.tab.PaneCount(); n != 1 {
		panes = fmt.Sprintf("%d panes", n)
	}
	return strings.Join([]string{
		m.eng.TabID(),
		panes,
		m.tab.LayoutName(),
		"this pane: " + shortID(m.eng.PaneID()),
	}, " · ")
}
