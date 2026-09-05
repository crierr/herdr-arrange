package ui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

// treeChrome is how many lines the tree view spends below the tree itself: a
// rule, two lines of help and the status message. The action preview is not among
// them: it rides on the selected row, where the eye already is, and the line that
// bought back goes to the tree.
const treeChrome = 4

// treeIndent is the left margin a row starts with, selection marker included, so
// the box-drawing prefixes do not sit against the popup's border. A row led by a
// bracketed shortcut gets one less, which lines its number up with the connectors
// below it — see renderRow.
const treeIndent = 3

// expandLevel is how far the tree is unfolded. One level for the whole tree rather
// than a fold per node: the tree is three deep and exists to be read at a glance, so
// what the user wants is usually "show me the workspaces" or "show me everything",
// not the state of forty separate triangles.
//
// The popup is sized for the deepest level either way (see treeSizeForRows), so
// folding never has to resize it and costs no flicker.
type expandLevel int

const (
	levelWorkspaces expandLevel = iota // the workspaces alone
	levelTabs                          // and the tabs in them; where the tree opens
	levelPanes                         // and the panes in those
)

func (l expandLevel) String() string {
	switch l {
	case levelWorkspaces:
		return "workspaces"
	case levelTabs:
		return "tabs"
	}
	return "panes"
}

// rowKind is what selecting a row does.
type rowKind int

const (
	rowWorkspace    rowKind = iota // make a new tab in this workspace
	rowTab                         // move the pane into this tab
	rowPane                        // move beside this pane, or swap with it
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

// depth is how deep in the tree a row sits, and so which fold level reveals it. The
// synthetic rows sit where their subject does: "new tab in this workspace" is a child
// of the workspace it would go in, "new workspace" a sibling of the workspaces.
func (r row) depth() int {
	switch r.kind {
	case rowTab, rowNewTabHere:
		return 1
	case rowPane:
		return 2
	}
	return 0
}

// rowKey identifies a row across rebuilds and fold levels, so a selection can outlive
// both.
type rowKey struct {
	kind                       rowKind
	workspaceID, tabID, paneID string
}

func (r row) key() rowKey {
	return rowKey{kind: r.kind, workspaceID: r.workspaceID, tabID: r.tabID, paneID: r.paneID}
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
			tabName := tab.Label
			if tabName == "" {
				tabName = shortID(tab.TabID)
			}
			t := row{
				kind:        rowTab,
				branch:      branch(lastTab),
				label:       join(shortID(tab.TabID), tab.Label),
				name:        tabName,
				detail:      panesWord(tab.PaneCount),
				workspaceID: ws.WorkspaceID,
				tabID:       tab.TabID,
				dim:         tab.TabID == tabID,
			}
			if t.dim {
				// The tree opens folded to the tabs, where the pane's own row is not
				// on screen to say where it lives. Dimming alone does not find it in a
				// column of tabs — a dim row reads as one more row — so the tab the
				// pane is in is marked the same way the pane is.
				t.note = "(current)"
			}
			rows = append(rows, t)

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

// setRows installs a freshly built tree, keeping the selection where it was.
func (m *Model) setRows(rows []row) {
	m.rows = rows
	m.selectWant()
	m.vp.Height = m.treeViewportHeight()
	m.syncViewport()
}

// visible reports whether the fold level shows a row.
func (m Model) visible(r row) bool { return r.depth() <= int(m.level) }

// selectWant puts the cursor on the row the user last picked, or as close to it as the
// session and the fold level allow: the nearest row above it, which — the tree being
// in order — is the parent that hides it. A row that is gone altogether hands the
// selection back to the pane being arranged, which is where the tree started.
//
// Keeping the selection as a row rather than an index is what lets folding be
// reversible: fold, and the cursor sits on the tab; unfold, and it is back on the pane.
func (m *Model) selectWant() {
	at := -1
	for i, r := range m.rows {
		if r.key() == m.want {
			at = i
			break
		}
	}
	if at < 0 {
		for i, r := range m.rows {
			if r.self {
				at = i
			}
		}
		at = max(at, 0)
		if at < len(m.rows) {
			m.want = m.rows[at].key()
		}
	}
	for at > 0 && at < len(m.rows) && !m.visible(m.rows[at]) {
		at--
	}
	m.cursor = at
}

func (m Model) treeViewportHeight() int {
	return max(1, m.height-treeChrome)
}

// syncViewport re-renders the rows on show and scrolls so the selection stays on
// screen. The cursor indexes the whole tree and the viewport only the folded-out part
// of it, so the two are counted apart.
func (m *Model) syncViewport() {
	shown := m.shown()
	lines := make([]string, len(shown))
	for at, i := range shown {
		lines[at] = m.renderRow(m.rows[i], i == m.cursor)
	}
	m.vp.SetContent(strings.Join(lines, "\n"))

	at := m.cursorLine()
	switch {
	// Folding can leave the whole tree shorter than the popup, and scrolled content
	// in a view with room to spare is just a missing top.
	case len(lines) <= m.vp.Height:
		m.vp.SetYOffset(0)
	case at < m.vp.YOffset:
		m.vp.SetYOffset(at)
	case at >= m.vp.YOffset+m.vp.Height:
		m.vp.SetYOffset(at - m.vp.Height + 1)
	}
}

// shown are the indices of the rows the fold level puts on screen, in order.
func (m Model) shown() []int {
	idx := make([]int, 0, len(m.rows))
	for i, r := range m.rows {
		if m.visible(r) {
			idx = append(idx, i)
		}
	}
	return idx
}

// cursorLine is which line of the viewport the selection is drawn on.
func (m Model) cursorLine() int { return max(slices.Index(m.shown(), m.cursor), 0) }

// renderRow draws one row, including the selection gutter and — on the selected
// row — what enter would do to it.
func (m Model) renderRow(r row, selected bool) string {
	t := m.theme

	// The selected row is never dimmed: legibility beats marking it as current.
	text, keyStyle := t.desc, t.key
	if r.dim && !selected {
		text, keyStyle = t.dim, t.dim
	}

	// And it is bold. The cursor is one thin mark at the left edge of a list of
	// near-identical rows; weight on the row's own words is what the eye finds.
	emph := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Bold(true)
		}
		return s
	}

	// A left margin, then the selection marker: the box-drawing prefixes read badly
	// against the popup's border.
	indent := treeIndent
	if r.shortcut != "" && r.branch == "" {
		// A workspace's "[1]" is a column wider than the "├─" of the tabs under it,
		// so it starts one column earlier and the number lines up with them.
		indent--
	}
	gutter := strings.Repeat(" ", indent)
	if selected {
		gutter = strings.Repeat(" ", indent-2) + t.cursor.Render("▸") + " "
	}

	body := t.dim.Render(r.branch)
	if r.shortcut != "" {
		body += keyStyle.Render("["+r.shortcut+"]") + " "
	}
	body += emph(text).Render(r.label)
	if r.detail != "" {
		body += emph(t.dim).Render("  " + r.detail)
	}
	if r.note != "" {
		body += "  " + emph(t.note).Render(r.note)
	}
	if selected {
		body += m.previewSuffix(indent + lipgloss.Width(body))
	}
	return gutter + body
}

// previewSuffix is the action preview, drawn at the end of the selected row rather
// than on a line of its own: it is about that row, and reading it means not looking
// away from the cursor.
//
// It is dropped rather than wrapped when the row leaves too little room, because a
// line that wraps pushes the bottom of the tree view off the popup.
func (m Model) previewSuffix(used int) string {
	const (
		gap = "  ← "
		// Below this much of a sentence an ellipsis says more than the words do.
		least = 8
	)

	room := m.width - used - 1
	if room < lipgloss.Width(gap)+least {
		return ""
	}
	return m.theme.state.Render(truncate(gap+m.treePreview(), room))
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

	case "l", "right":
		return m.foldTo(m.level + 1), nil
	case "h", "left":
		return m.foldTo(m.level - 1), nil

	case "enter":
		if m.cursor < len(m.rows) {
			where := nextLayout
			if m.rows[m.cursor].kind == rowWorkspace {
				where = nextQuit
			}
			return m.act(m.rows[m.cursor], where)
		}
		return m, nil

	case "s":
		if m.cursor < len(m.rows) {
			return m.swapWith(m.rows[m.cursor])
		}
		return m, nil

	case "t":
		if m.currentTabPanes() < 2 {
			return m.flashSinglePane()
		}
		m.status, m.statusKind = "", statusNone
		return m.switchTo(ModeLayout)

	case "c":
		return m.jump(func(r row) bool { return r.kind == rowNewTabHere }, nextQuit)
	case "N":
		return m.jump(func(r row) bool { return r.kind == rowNewWorkspace }, nextQuit)
	}

	// 1-9 select a workspace directly.
	if n, err := strconv.Atoi(msg.String()); err == nil && n >= 1 && n <= 9 {
		return m.jump(func(r row) bool { return r.kind == rowWorkspace && r.shortcut == strconv.Itoa(n) }, nextQuit)
	}
	return m, nil
}

// moveCursor moves the selection by delta rows of the tree as it is folded, clamped
// to the ends.
func (m Model) moveCursor(delta int) Model {
	shown := m.shown()
	if len(shown) == 0 {
		return m
	}
	m.cursor = shown[min(max(m.cursorLine()+delta, 0), len(shown)-1)]
	m.want = m.rows[m.cursor].key()
	m.syncViewport()
	return m
}

// foldTo unfolds the tree one level further, or folds it one back. The selection is
// re-resolved rather than moved, so unfolding lands on the row the fold was hiding:
// open the tree, fold to the workspaces and unfold again, and the cursor is back on
// the pane being arranged.
func (m Model) foldTo(level expandLevel) Model {
	if level = min(max(level, levelWorkspaces), levelPanes); level == m.level {
		return m
	}
	m.level = level
	m.status, m.statusKind = "showing "+level.String(), statusInfo
	m.selectWant()
	m.syncViewport()
	return m
}

// jump moves the selection to a shortcut's row and acts on it, so the user sees
// what the key did.
func (m Model) jump(match func(row) bool, where next) (tea.Model, tea.Cmd) {
	for _, r := range m.rows {
		if match(r) {
			// The row may be folded away — 1-9 and [c] work at any level — so ask for
			// it and take whatever stands in for it on screen.
			m.want = r.key()
			m.selectWant()
			m.syncViewport()
			return m.act(r, where)
		}
	}
	return m, nil
}

// act performs the selected row's action.
func (m Model) act(r row, where next) (tea.Model, tea.Cmd) {
	eng := m.eng

	switch r.kind {
	case rowWorkspace:
		ws, name := r.workspaceID, r.name
		return m, m.op(where, func(ctx context.Context) (string, error) {
			return "moved to a new tab in " + name, eng.MoveToNewTab(ctx, ws)
		})

	case rowNewTabHere:
		return m, m.op(nextQuit, func(ctx context.Context) (string, error) {
			return "moved to a new tab", eng.MoveToNewTab(ctx, "")
		})

	case rowNewWorkspace:
		return m, m.op(nextQuit, func(ctx context.Context) (string, error) {
			return "moved to a new workspace", eng.MoveToNewWorkspace(ctx)
		})

	case rowTab:
		tabID := r.tabID
		return m, m.op(nextLayout, func(ctx context.Context) (string, error) {
			return "moved to " + shortID(tabID), eng.MoveToTab(ctx, tabID)
		})

	case rowPane:
		if r.self {
			// Per spec: the current pane leads back to layout mode, and does
			// nothing at all when there is no layout to arrange.
			if m.currentTabPanes() < 2 {
				return m.flashSinglePane()
			}
			m.status, m.statusKind = "", statusNone
			return m.switchTo(ModeLayout)
		}
		// Every other pane is somewhere to land, in this tab as much as in another
		// one: one row kind, one meaning. Trading places with it instead is `s` —
		// see swapWith.
		tabID, target := r.tabID, r.paneID
		return m, m.op(nextLayout, func(ctx context.Context) (string, error) {
			return "moved next to " + shortID(target), eng.MoveBesidePane(ctx, tabID, target)
		})
	}
	return m, nil
}

// swapWith handles `s`: the two panes trade places, each taking the other's slot at
// the other's size, where enter would move this pane beside the other one.
//
// It is a key of its own rather than what enter does on a pane row, because a tree
// of panes should not mean one thing in this tab and another everywhere else. It
// works on any pane in the session — see engine.SwapWithPane, which trades across a
// tab boundary that herdr's own pane.swap will not cross.
//
// A tab row stands for the first pane in it, the way enter on a tab stands for
// "into that tab": swapping with a tab you have not unfolded is a reasonable thing
// to mean, and the first pane is the one the tree would show you under it.
func (m Model) swapWith(r row) (tea.Model, tea.Cmd) {
	if r.kind == rowTab {
		first, ok := m.firstPaneOf(r.tabID)
		if !ok {
			return m.flash("that tab has no pane to swap with")
		}
		r = first
	}
	switch {
	case r.kind != rowPane:
		return m.flash("select a pane or a tab to swap with")
	case r.self:
		return m.flash("select another pane to swap with")
	}

	eng, tabID, target := m.eng, r.tabID, r.paneID
	// A swap is the whole request — both panes are placed when it lands, with
	// nothing left to arrange — so the popup closes afterwards.
	return m, m.op(nextQuit, func(ctx context.Context) (string, error) {
		return "", eng.SwapWithPane(ctx, tabID, target)
	})
}

// firstPaneOf returns a tab's first pane row, which is the one drawn directly under
// the tab when the tree is unfolded.
func (m Model) firstPaneOf(tabID string) (row, bool) {
	for _, r := range m.rows {
		if r.kind == rowPane && r.tabID == tabID {
			return r, true
		}
	}
	return row{}, false
}

// flash says why a key did nothing, without treating it as a failure.
func (m Model) flash(why string) (tea.Model, tea.Cmd) {
	m.status, m.statusKind = why, statusFlash
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
//
// Every line starts with the verb — "move to …" — because that is the one thing the
// tree does, and reading down a column of them says where each row would send the
// pane. It reads as an annotation of the row it is drawn on, so it names the
// destination the way the row does and leaves out "this pane", which is the only
// pane in play: that is also what keeps it short enough to sit beside the row.
func (m Model) treePreview() string {
	if len(m.rows) == 0 || m.cursor >= len(m.rows) {
		return "reading the session…"
	}

	r := m.rows[m.cursor]
	switch r.kind {
	case rowWorkspace:
		// Both make a tab, but only in another workspace is the workspace the news;
		// in this one the tab is, which is what the [c] row below it also says.
		if r.workspaceID == m.eng.WorkspaceID() {
			return "move to a new tab"
		}
		return "move to workspace " + r.name
	case rowNewTabHere:
		return "move to a new tab"
	case rowNewWorkspace:
		return "move to a new workspace"
	case rowTab:
		if r.tabID == m.eng.TabID() {
			return "already in tab " + r.name
		}
		return "move to tab " + r.name
	case rowPane:
		switch {
		case r.self && m.currentTabPanes() > 1:
			return "back to layout mode"
		case r.self:
			return "the pane you are moving"
		default:
			// Both keys, on the row they act on: any pane is somewhere to land, and
			// the same pane is someone to trade places with.
			return "move next to " + shortID(r.paneID) + ", press s to swap"
		}
	}
	return ""
}

// treeView renders tree mode: the scrolling tree over its help. What enter would
// do is on the selected row, not down here — see previewSuffix.
func (m Model) treeView() string {
	t := m.theme
	kv := func(key, desc string) string {
		return t.key.Render(key) + " " + t.desc.Render(desc)
	}

	return strings.Join([]string{
		m.vp.View(),
		t.rules(m.width),
		" " + strings.Join([]string{kv("j/k", "move"), kv("h/l", "fold"), kv("enter", "apply"), kv("s", "swap"), kv("esc", "close")}, "  "),
		" " + strings.Join([]string{kv("c", "new tab here"), kv("1-9", "workspace"), kv("N", "new workspace"), kv("t", "layout")}, "  "),
		" " + m.message(),
	}, "\n")
}
