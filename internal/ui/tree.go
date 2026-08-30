package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

// treeChrome is how many lines the tree view spends below the tree itself: a
// rule, two lines of help, the action preview and the status message.
const treeChrome = 5

// rowKind is what selecting a row does.
type rowKind int

const (
	rowWorkspace    rowKind = iota // make a new tab in this workspace
	rowTab                         // move the pane into this tab
	rowPane                        // swap with, or move beside, this pane
	rowNewTabHere                  // make a new tab in the current workspace
	rowNewWorkspace                // make a new workspace
)

// row is one line of the tree. The whole view is a flat list, because every row
// is selectable and the tree is only ever three levels deep.
type row struct {
	kind     rowKind
	branch   string // box-drawing prefix
	shortcut string // "1"-"9", "c" or "N"; empty when the row has no direct key
	label    string // as rendered, id and label together
	name     string // just the human part, for prose
	detail   string // a dim suffix, such as the pane count
	note     string // "(current)"

	workspaceID string
	tabID       string
	paneID      string

	dim     bool // the workspace, tab or pane the arranged pane is already in
	self    bool // the pane being arranged
	sameTab bool // a pane sharing a tab with the arranged pane
}

// buildRows turns a session snapshot into the tree view, in snapshot order.
func buildRows(s *herdr.SessionSnapshot, paneID, tabID, workspaceID string) []row {
	tabsOf := map[string][]herdr.TabInfo{}
	for _, tab := range s.Tabs {
		tabsOf[tab.WorkspaceID] = append(tabsOf[tab.WorkspaceID], tab)
	}
	panesOf := map[string][]herdr.PaneInfo{}
	for _, pane := range s.Panes {
		panesOf[pane.TabID] = append(panesOf[pane.TabID], pane)
	}

	branch := func(last bool) string {
		if last {
			return "└─ "
		}
		return "├─ "
	}

	var rows []row
	for i, ws := range s.Workspaces {
		here := ws.WorkspaceID == workspaceID

		shortcut := ""
		if i < 9 {
			shortcut = strconv.Itoa(i + 1)
		}
		name := ws.Label
		if name == "" {
			name = ws.WorkspaceID
		}
		rows = append(rows, row{
			kind:        rowWorkspace,
			shortcut:    shortcut,
			label:       join(ws.WorkspaceID, ws.Label),
			name:        name,
			workspaceID: ws.WorkspaceID,
			dim:         here,
		})

		tabs := tabsOf[ws.WorkspaceID]
		for j, tab := range tabs {
			// The current workspace gets a trailing "new tab" row, so its last
			// tab is not the last child.
			lastTab := j == len(tabs)-1 && !here
			rows = append(rows, row{
				kind:        rowTab,
				branch:      branch(lastTab),
				label:       join(shortID(tab.TabID), tab.Label),
				detail:      panesWord(tab.PaneCount),
				workspaceID: ws.WorkspaceID,
				tabID:       tab.TabID,
				dim:         tab.TabID == tabID,
			})

			indent := "│  "
			if lastTab {
				indent = "   "
			}
			panes := panesOf[tab.TabID]
			for k, pane := range panes {
				r := row{
					kind:        rowPane,
					branch:      indent + branch(k == len(panes)-1),
					label:       join(shortID(pane.PaneID), paneName(pane)),
					workspaceID: ws.WorkspaceID,
					tabID:       tab.TabID,
					paneID:      pane.PaneID,
					self:        pane.PaneID == paneID,
					sameTab:     tab.TabID == tabID,
				}
				if r.self {
					r.note, r.dim = "(current)", true
				}
				rows = append(rows, r)
			}
		}

		if here {
			rows = append(rows, row{
				kind:        rowNewTabHere,
				branch:      branch(true),
				shortcut:    "c",
				label:       "new tab in this workspace",
				workspaceID: ws.WorkspaceID,
			})
		}
	}

	return append(rows, row{kind: rowNewWorkspace, shortcut: "N", label: "new workspace"})
}

// join glues an id to a label, skipping the gap when there is no label.
func join(id, label string) string {
	if label == "" {
		return id
	}
	return id + "  " + label
}

// paneName is a pane's human name, or nothing when herdr only knows its id.
func paneName(p herdr.PaneInfo) string {
	if name := p.DisplayName(); name != p.PaneID {
		return name
	}
	return ""
}

func panesWord(n int) string {
	if n == 1 {
		return "1 pane"
	}
	return fmt.Sprintf("%d panes", n)
}

// setRows installs a freshly built tree, keeping the selection on the same row
// where it still exists and falling back to the pane being arranged.
func (m *Model) setRows(rows []row) {
	var was row
	if m.cursor < len(m.rows) {
		was = m.rows[m.cursor]
	}
	m.rows = rows

	m.cursor = 0
	for i, r := range rows {
		if r.self {
			m.cursor = i
		}
	}
	for i, r := range rows {
		if r.kind == was.kind && r.workspaceID == was.workspaceID && r.tabID == was.tabID && r.paneID == was.paneID {
			m.cursor = i
			break
		}
	}

	m.vp.Height = m.treeViewportHeight()
	m.syncViewport()
}

func (m Model) treeViewportHeight() int {
	return max(1, m.height-treeChrome)
}

// syncViewport re-renders the rows and scrolls so the selection stays visible.
func (m *Model) syncViewport() {
	lines := make([]string, len(m.rows))
	for i, r := range m.rows {
		lines[i] = m.renderRow(r, i == m.cursor)
	}
	m.vp.SetContent(strings.Join(lines, "\n"))

	switch {
	case m.cursor < m.vp.YOffset:
		m.vp.SetYOffset(m.cursor)
	case m.cursor >= m.vp.YOffset+m.vp.Height:
		m.vp.SetYOffset(m.cursor - m.vp.Height + 1)
	}
}

// renderRow draws one row, including the two-column selection gutter.
func (m Model) renderRow(r row, selected bool) string {
	t := m.theme

	// The selected row is never dimmed: legibility beats marking it as current.
	text, keyStyle := t.desc, t.key
	if r.dim && !selected {
		text, keyStyle = t.dim, t.dim
	}

	gutter := "  "
	if selected {
		gutter = t.cursor.Render("▸ ")
	}

	body := t.dim.Render(r.branch)
	if r.shortcut != "" {
		body += keyStyle.Render("["+r.shortcut+"]") + " "
	}
	body += text.Render(r.label)
	if r.detail != "" {
		body += t.dim.Render("  " + r.detail)
	}
	if r.note != "" {
		body += "  " + t.note.Render(r.note)
	}
	return gutter + body
}

// treeKey handles a keypress in tree mode.
func (m Model) treeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := max(1, m.vp.Height)

	switch key := msg.String(); key {
	case "j", "down":
		return m.moveCursor(1), nil
	case "k", "up":
		return m.moveCursor(-1), nil
	case "g", "home":
		return m.moveCursor(-len(m.rows)), nil
	case "G", "end":
		return m.moveCursor(len(m.rows)), nil
	case "pgdown", "ctrl+f":
		return m.moveCursor(page), nil
	case "pgup", "ctrl+b":
		return m.moveCursor(-page), nil
	case "ctrl+d":
		return m.moveCursor(page / 2), nil
	case "ctrl+u":
		return m.moveCursor(-page / 2), nil

	case "enter":
		if m.cursor < len(m.rows) {
			return m.act(m.rows[m.cursor])
		}
		return m, nil

	case "t":
		if m.currentTabPanes() < 2 {
			return m.flashSinglePane()
		}
		m.status, m.statusKind = "", statusNone
		return m.switchTo(ModeLayout)

	case "c":
		return m.jump(func(r row) bool { return r.kind == rowNewTabHere })
	case "N":
		return m.jump(func(r row) bool { return r.kind == rowNewWorkspace })
	}

	// 1-9 select a workspace directly.
	if n, err := strconv.Atoi(msg.String()); err == nil && n >= 1 && n <= 9 {
		return m.jump(func(r row) bool { return r.kind == rowWorkspace && r.shortcut == strconv.Itoa(n) })
	}
	return m, nil
}

// moveCursor moves the selection by delta rows, clamped to the list.
func (m Model) moveCursor(delta int) Model {
	if len(m.rows) == 0 {
		return m
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)
	m.syncViewport()
	return m
}

// jump moves the selection to a shortcut's row and acts on it, so the user sees
// what the key did.
func (m Model) jump(match func(row) bool) (tea.Model, tea.Cmd) {
	for i, r := range m.rows {
		if match(r) {
			m.cursor = i
			m.syncViewport()
			return m.act(r)
		}
	}
	return m, nil
}

// act performs the selected row's action.
func (m Model) act(r row) (tea.Model, tea.Cmd) {
	eng := m.eng

	switch r.kind {
	case rowWorkspace:
		ws, name := r.workspaceID, r.name
		return m, m.op(nextLayout, func(ctx context.Context) (string, error) {
			return "moved to a new tab in " + name, eng.MoveToNewTab(ctx, ws)
		})

	case rowNewTabHere:
		return m, m.op(nextLayout, func(ctx context.Context) (string, error) {
			return "moved to a new tab", eng.MoveToNewTab(ctx, "")
		})

	case rowNewWorkspace:
		return m, m.op(nextLayout, func(ctx context.Context) (string, error) {
			return "moved to a new workspace", eng.MoveToNewWorkspace(ctx)
		})

	case rowTab:
		tabID := r.tabID
		return m, m.op(nextLayout, func(ctx context.Context) (string, error) {
			return "moved to " + shortID(tabID), eng.MoveToTab(ctx, tabID)
		})

	case rowPane:
		switch {
		case r.self:
			// Per spec: the current pane leads back to layout mode, and does
			// nothing at all when there is no layout to arrange.
			if m.currentTabPanes() < 2 {
				return m.flashSinglePane()
			}
			m.status, m.statusKind = "", statusNone
			return m.switchTo(ModeLayout)

		case r.sameTab:
			// A same-tab swap is what the user came for; close afterwards.
			target := r.paneID
			return m, m.op(nextQuit, func(ctx context.Context) (string, error) {
				return "", eng.SwapWithPane(ctx, target)
			})

		default:
			// pane.swap cannot cross tabs, so selecting a pane elsewhere puts
			// this pane next to it instead.
			tabID, target := r.tabID, r.paneID
			return m, m.op(nextLayout, func(ctx context.Context) (string, error) {
				return "moved next to " + shortID(target), eng.MoveToTabBeside(ctx, tabID, target)
			})
		}
	}
	return m, nil
}

// currentTabPanes counts the panes sharing a tab with the arranged pane, from the
// snapshot the tree was built from.
func (m Model) currentTabPanes() int {
	n := 0
	for _, r := range m.rows {
		if r.kind == rowPane && r.sameTab {
			n++
		}
	}
	return n
}

// treePreview states exactly what enter will do, which is the only way a tree of
// mixed row kinds stays predictable.
func (m Model) treePreview() string {
	if len(m.rows) == 0 || m.cursor >= len(m.rows) {
		return "reading the session…"
	}

	r := m.rows[m.cursor]
	switch r.kind {
	case rowWorkspace:
		return "→ new tab in workspace " + r.name
	case rowNewTabHere:
		return "→ new tab in this workspace"
	case rowNewWorkspace:
		return "→ move this pane to a new workspace"
	case rowTab:
		if r.tabID == m.eng.TabID() {
			return "→ this pane is already in " + shortID(r.tabID)
		}
		return "→ move this pane to " + shortID(r.tabID)
	case rowPane:
		switch {
		case r.self && m.currentTabPanes() > 1:
			return "→ back to layout mode"
		case r.self:
			return "→ this is the pane you are moving"
		case r.sameTab:
			return "→ swap this pane with " + shortID(r.paneID)
		default:
			return "→ move this pane next to " + shortID(r.paneID)
		}
	}
	return ""
}

// treeView renders tree mode: the scrolling tree over its help and preview.
func (m Model) treeView() string {
	t := m.theme
	kv := func(key, desc string) string {
		return t.key.Render(key) + " " + t.desc.Render(desc)
	}

	return strings.Join([]string{
		m.vp.View(),
		t.rules(m.width),
		" " + strings.Join([]string{kv("j/k", "move"), kv("enter", "apply"), kv("t", "layout"), kv("esc", "close")}, "  "),
		" " + strings.Join([]string{kv("c", "new tab here"), kv("1-9", "workspace"), kv("N", "new workspace")}, "  "),
		" " + t.state.Render(truncate(m.treePreview(), m.width-1)),
		" " + m.message(),
	}, "\n")
}
