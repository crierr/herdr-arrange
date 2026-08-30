// Package ui is the Bubble Tea application that runs inside herdr's popup.
//
// It has two views. Layout mode arranges the panes of the current tab; tree mode
// sends the pane to another tab or workspace. Both drive the same engine, and
// every action re-reads herdr afterwards rather than tracking state locally, so
// the popup cannot drift from what the user is looking at behind it.
package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/crierr/herdr-arrange/internal/engine"
	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

// Mode is which view the popup shows.
type Mode int

const (
	// ModeLayout arranges the panes of the current tab.
	ModeLayout Mode = iota
	// ModeTree moves the pane to another tab or workspace.
	ModeTree
)

// opTimeout bounds one user action. A rebuild is ~20 socket calls, each of which
// the client already times out individually; this is the backstop for a whole
// plan, so a wedged server cannot leave the popup unresponsive forever.
const opTimeout = 30 * time.Second

// next is where an action leaves the popup once it has finished.
type next int

const (
	nextStay next = iota // reload the current view
	nextLayout
	nextTree
	nextQuit
)

// statusKind selects how the bottom line is styled.
type statusKind int

const (
	statusNone statusKind = iota
	statusInfo
	statusFlash // nothing to do; not an error
	statusFail
)

// Model is the popup's state.
type Model struct {
	eng   *engine.Engine
	theme theme
	mode  Mode

	width, height int

	// tab is layout mode's data, re-read after every action.
	tab *engine.Tab

	// rows and cursor are tree mode's data, derived from a session snapshot.
	rows   []row
	cursor int
	vp     viewport.Model

	status     string
	statusKind statusKind
	busy       bool

	// fatal ends the session: the pane being arranged no longer exists, so
	// there is nothing left to act on.
	fatal    error
	quitting bool
}

// New returns a popup model starting in the given mode.
func New(eng *engine.Engine, mode Mode) Model {
	return Model{
		eng:   eng,
		theme: newTheme(),
		mode:  mode,
		// Sensible until the first WindowSizeMsg arrives.
		width:  60,
		height: 16,
		vp:     viewport.New(60, 8),
	}
}

// Run starts the popup and blocks until the user closes it.
func Run(eng *engine.Engine, mode Mode) error {
	final, err := tea.NewProgram(New(eng, mode), tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	m, ok := final.(Model)
	if !ok || m.fatal == nil {
		return nil
	}
	// A pane closing while the popup is open is ordinary, not a plugin failure.
	if errors.Is(m.fatal, engine.ErrPaneGone) {
		return nil
	}
	return m.fatal
}

func (m Model) Init() tea.Cmd { return m.reload() }

// messages

// tabMsg carries the result of reading the current tab.
type tabMsg struct {
	tab *engine.Tab
	err error
}

// snapshotMsg carries the result of reading the whole session.
type snapshotMsg struct {
	snapshot *herdr.SessionSnapshot
	err      error
}

// opMsg reports a finished action.
type opMsg struct {
	status string
	err    error
	next   next
}

// reload fetches whatever the current mode renders.
func (m Model) reload() tea.Cmd {
	eng := m.eng
	if m.mode == ModeTree {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
			defer cancel()
			snapshot, err := eng.Snapshot(ctx)
			return snapshotMsg{snapshot: snapshot, err: err}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		tab, err := eng.Tab(ctx)
		return tabMsg{tab: tab, err: err}
	}
}

// op runs an action off the update loop. fn returns the status line to show on
// success, so an action that only discovers what it did while doing it — cycling
// presets, say — can report it.
func (m *Model) op(where next, fn func(context.Context) (string, error)) tea.Cmd {
	m.busy = true
	m.status = ""
	m.statusKind = statusNone
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()
		status, err := fn(ctx)
		return opMsg{status: status, err: err, next: where}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.vp.Width = msg.Width
		m.vp.Height = m.treeViewportHeight()
		m.syncViewport()
		return m, nil

	case tea.KeyMsg:
		return m.key(msg)

	case tabMsg:
		if msg.err != nil {
			return m.failed(msg.err)
		}
		m.tab = msg.tab
		return m, nil

	case snapshotMsg:
		if msg.err != nil {
			return m.failed(msg.err)
		}
		m.setRows(buildRows(msg.snapshot, m.eng.PaneID(), m.eng.TabID(), m.eng.WorkspaceID()))
		return m, nil

	case opMsg:
		return m.finished(msg)
	}
	return m, nil
}

// key dispatches a keypress to the active mode. Keys that mean the same thing
// everywhere are handled here.
func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// An action is in flight: swallow everything except a way out, so a second
	// keypress cannot start a rebuild on top of one already running.
	if m.busy {
		if msg.Type == tea.KeyCtrlC {
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil
	}
	if m.fatal != nil {
		m.quitting = true
		return m, tea.Quit
	}

	switch msg.String() {
	case "ctrl+c", "esc", "q":
		m.quitting = true
		return m, tea.Quit
	}

	if m.mode == ModeTree {
		return m.treeKey(msg)
	}
	return m.layoutKey(msg)
}

// finished folds an action's result back into the model.
func (m Model) finished(msg opMsg) (tea.Model, tea.Cmd) {
	m.busy = false

	switch {
	case msg.err == nil:
		m.status, m.statusKind = msg.status, statusInfo

	case errors.Is(msg.err, engine.ErrPaneGone):
		return m.failed(msg.err)

	case errors.Is(msg.err, tree.ErrNoChange):
		// Not a failure: the key simply had nothing to do here.
		m.status, m.statusKind = flashText(msg.err), statusFlash
		return m, m.reload()

	default:
		// The action may have changed part of the tab before failing, so reload
		// rather than leaving a stale view on screen.
		m.status, m.statusKind = msg.err.Error(), statusFail
		return m, m.reload()
	}

	if msg.next == nextQuit {
		m.quitting = true
		return m, tea.Quit
	}
	switch msg.next {
	case nextLayout:
		m.mode = ModeLayout
	case nextTree:
		m.mode = ModeTree
	}
	if m.mode == ModeTree {
		m.vp.Height = m.treeViewportHeight()
	}
	return m, m.reload()
}

// failed records an unrecoverable condition and closes the popup.
func (m Model) failed(err error) (tea.Model, tea.Cmd) {
	m.busy = false
	m.fatal = err
	m.quitting = true
	return m, tea.Quit
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}
	body := m.layoutView()
	if m.mode == ModeTree {
		body = m.treeView()
	}
	// A popup narrower than the help would wrap it, and a wrapped line pushes
	// everything below it off the bottom. Clip instead.
	return lipgloss.NewStyle().MaxWidth(m.width).Render(body)
}

// statusHeight is how many lines statusLines renders.
const statusHeight = 3

// statusLines renders the two reserved bottom lines, under a rule: what the popup
// is looking at, and the last thing that happened. Both are always present, so the
// help above them never shifts as messages come and go.
func (m Model) statusLines(state string) string {
	t := m.theme
	return t.rules(m.width) + "\n " + t.state.Render(state) + "\n " + m.message()
}

// message is the transient bottom line: progress, a result, or a failure.
func (m Model) message() string {
	t := m.theme
	switch {
	case m.busy:
		return t.busy.Render("working…")
	case m.statusKind == statusInfo:
		return t.desc.Render(truncate(m.status, m.width-1))
	case m.statusKind == statusFlash:
		return t.flash.Render(truncate(m.status, m.width-1))
	case m.statusKind == statusFail:
		return t.fail.Render(truncate(m.status, m.width-1))
	}
	return ""
}

// flashText turns a no-change error into something worth putting on one line:
// "no pane to the left: no change" reads better as "no pane to the left".
func flashText(err error) string {
	msg := err.Error()
	suffix := ": " + tree.ErrNoChange.Error()
	if trimmed := strings.TrimSuffix(msg, suffix); trimmed != msg {
		return trimmed
	}
	if msg == tree.ErrNoChange.Error() {
		return "nothing to change"
	}
	return msg
}

// shortID drops the workspace prefix from a herdr id: "w1S:p2" reads as "p2" in
// a popup that is already narrow, and the workspace is on screen anyway.
func shortID(id string) string {
	if i := strings.LastIndex(id, ":"); i >= 0 {
		return id[i+1:]
	}
	return id
}

// truncate shortens a line to fit the popup, with an ellipsis.
func truncate(s string, width int) string {
	if width < 1 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
