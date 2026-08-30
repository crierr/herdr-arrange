package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// theme holds every style the popup uses.
//
// Colours are ANSI 0-15 rather than hex, so the popup inherits whatever palette
// the user's terminal and herdr theme already use instead of fighting it.
type theme struct {
	key    lipgloss.Style // a keybinding
	desc   lipgloss.Style // what the key does
	active lipgloss.Style // the layout the tab currently matches
	dim    lipgloss.Style // disabled keys, and the current workspace/tab/pane
	rule   lipgloss.Style
	state  lipgloss.Style // the "w1S:t1 · 4 panes · …" line
	flash  lipgloss.Style // "there is nothing to do"
	fail   lipgloss.Style // an operation failed
	busy   lipgloss.Style
	cursor lipgloss.Style // the selected row in the tree view
	note   lipgloss.Style // "(current)"
}

func newTheme() theme {
	return theme{
		key:    lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		desc:   lipgloss.NewStyle(),
		active: lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true),
		dim:    lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		rule:   lipgloss.NewStyle().Foreground(lipgloss.Color("8")),
		state:  lipgloss.NewStyle().Foreground(lipgloss.Color("4")),
		flash:  lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
		fail:   lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true),
		busy:   lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
		cursor: lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true),
		note:   lipgloss.NewStyle().Foreground(lipgloss.Color("5")),
	}
}

// keyOr returns the key style, or the dim one when the binding does nothing in
// the current state.
func (t theme) keyOr(enabled bool) lipgloss.Style {
	if enabled {
		return t.key
	}
	return t.dim
}

// descOr is keyOr for a description.
func (t theme) descOr(enabled bool) lipgloss.Style {
	if enabled {
		return t.desc
	}
	return t.dim
}

// rules renders a horizontal rule.
func (t theme) rules(width int) string {
	if width < 1 {
		width = 1
	}
	return t.rule.Render(strings.Repeat("─", width))
}

// pad widens a styled cell to a column width, so help text lines up without
// counting escape sequences by hand.
func pad(s string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(s)
}
