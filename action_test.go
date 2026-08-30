package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/ui"
)

// fakeOpener stands in for the socket while testing what the action asks for.
type fakeOpener struct {
	snapshot *herdr.SessionSnapshot
	opened   []herdr.PopupOptions
	notes    []string
	openErr  error
}

func (f *fakeOpener) Snapshot(context.Context) (*herdr.SessionSnapshot, error) {
	return f.snapshot, nil
}

func (f *fakeOpener) OpenPopup(_ context.Context, opts herdr.PopupOptions) error {
	f.opened = append(f.opened, opts)
	return f.openErr
}

func (f *fakeOpener) Notify(_ context.Context, title, body string) error {
	f.notes = append(f.notes, title+": "+body)
	return nil
}

// session builds a snapshot whose first tab holds count panes and whose second
// holds one, which is enough to exercise the mode decision.
func session(count int) *herdr.SessionSnapshot {
	s := &herdr.SessionSnapshot{
		Workspaces: []herdr.WorkspaceInfo{{WorkspaceID: "w1S", Number: 1, Label: "work", ActiveTabID: "w1S:t1"}},
		Tabs: []herdr.TabInfo{
			{TabID: "w1S:t1", WorkspaceID: "w1S", Number: 1, PaneCount: count},
			{TabID: "w1S:t2", WorkspaceID: "w1S", Number: 2, PaneCount: 1},
		},
		Panes: []herdr.PaneInfo{{PaneID: "w1S:p9", TabID: "w1S:t2", WorkspaceID: "w1S"}},
	}
	for i := 1; i <= count; i++ {
		s.Panes = append(s.Panes, herdr.PaneInfo{
			PaneID: "w1S:p" + string(rune('0'+i)), TabID: "w1S:t1", WorkspaceID: "w1S",
		})
	}
	return s
}

func here() target {
	return target{PaneID: "w1S:p1", TabID: "w1S:t1", WorkspaceID: "w1S"}
}

func openOne(t *testing.T, f *fakeOpener, mode ui.Mode) herdr.PopupOptions {
	t.Helper()
	if err := open(context.Background(), f, here(), mode); err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(f.opened) != 1 {
		t.Fatalf("opened %d popups, want 1", len(f.opened))
	}
	return f.opened[0]
}

// TestOpenAsksForTheSizeTheViewNeeds ties the popup geometry to the views, so the
// help panel and the popup it lives in cannot drift apart.
func TestOpenAsksForTheSizeTheViewNeeds(t *testing.T) {
	f := &fakeOpener{snapshot: session(4)}
	opts := openOne(t, f, ui.ModeLayout)

	width, height := ui.LayoutPopupSize()
	if *opts.Width != herdr.Cells(width) || *opts.Height != herdr.Cells(height) {
		t.Errorf("layout mode asked for %v x %v, want %d x %d", *opts.Width, *opts.Height, width, height)
	}

	f = &fakeOpener{snapshot: session(4)}
	opts = openOne(t, f, ui.ModeTree)

	width, height = ui.TreePopupSize(f.snapshot, "w1S:p1", "w1S:t1", "w1S")
	if *opts.Width != herdr.Cells(width) || *opts.Height != herdr.Cells(height) {
		t.Errorf("tree mode asked for %v x %v, want %d x %d", *opts.Width, *opts.Height, width, height)
	}
}

// TestOpenTellsThePopupWhatToArrange: the popup is not a pane, so this env is the
// only way it learns which pane the user was on.
func TestOpenTellsThePopupWhatToArrange(t *testing.T) {
	f := &fakeOpener{snapshot: session(4)}
	opts := openOne(t, f, ui.ModeLayout)

	want := map[string]string{
		"ARRANGE_MODE": "layout",
		"ARRANGE_PANE": "w1S:p1",
		"ARRANGE_TAB":  "w1S:t1",
		"ARRANGE_WS":   "w1S",
	}
	for key, value := range want {
		if opts.Env[key] != value {
			t.Errorf("env %s = %q, want %q", key, opts.Env[key], value)
		}
	}
	if opts.Entrypoint != uiEntrypointID || opts.PluginID == "" || !opts.Focus {
		t.Errorf("popup opened as %+v", opts)
	}
}

// TestOpenOnASinglePaneTabStartsInTheTree: there is no layout to arrange, and the
// tree is the one view with something to offer.
func TestOpenOnASinglePaneTabStartsInTheTree(t *testing.T) {
	f := &fakeOpener{snapshot: session(1)}
	opts := openOne(t, f, ui.ModeLayout)

	if opts.Env["ARRANGE_MODE"] != "tree" {
		t.Errorf("opened in %q mode", opts.Env["ARRANGE_MODE"])
	}
	// Tree mode was asked for explicitly, so it stays tree mode either way.
	f = &fakeOpener{snapshot: session(1)}
	if got := openOne(t, f, ui.ModeTree).Env["ARRANGE_MODE"]; got != "tree" {
		t.Errorf("opened in %q mode", got)
	}
}

// TestOpenExplainsARefusedPopup: nothing watches the action's exit code, so a
// keypress that cannot open the popup has to say why itself.
func TestOpenExplainsARefusedPopup(t *testing.T) {
	cases := []struct {
		what string
		err  error
		want string
	}{
		{"a dialog is open", &herdr.APIError{Code: "ui_busy", Message: "popup panes can only open from the normal workspace view"},
			"close the current herdr dialog first"},
		{"a popup is open", &herdr.APIError{Code: "plugin_pane_open_failed", Message: "popup already open"},
			"the arrange popup is already open"},
		{"the terminal is tiny", &herdr.APIError{Code: "plugin_pane_open_failed", Message: "terminal area too small for popup"},
			"the terminal is too small for the arrange popup"},
		{"something else", errors.New("socket went away"), "socket went away"},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			f := &fakeOpener{snapshot: session(4), openErr: c.err}
			err := open(context.Background(), f, here(), ui.ModeLayout)

			if !errors.Is(err, c.err) {
				t.Errorf("open returned %v, want the underlying failure", err)
			}
			if len(f.notes) != 1 || !strings.Contains(f.notes[0], c.want) {
				t.Errorf("notifications were %v, want one containing %q", f.notes, c.want)
			}
		})
	}
}

// TestResolveTargetPrefersTheActionsEnv: the popup is told which pane to arrange
// explicitly, because by the time it starts the focus is on the popup itself.
func TestResolveTargetPrefersTheActionsEnv(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"workspace_id":"w9Q","tab_id":"w9Q:t1","focused_pane_id":"w9Q:p1"}`)
	t.Setenv("ARRANGE_PANE", "w1S:p2")
	t.Setenv("ARRANGE_TAB", "w1S:t1")
	t.Setenv("ARRANGE_WS", "w1S")
	t.Setenv("ARRANGE_MODE", "tree")

	got, err := resolveTarget()
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if want := (target{Mode: ui.ModeTree, PaneID: "w1S:p2", TabID: "w1S:t1", WorkspaceID: "w1S"}); got != want {
		t.Errorf("target = %+v, want %+v", got, want)
	}
}

// TestResolveTargetFallsBackToHerdrsContext is what makes `arrange ui` runnable by
// hand, and what an action itself uses.
func TestResolveTargetFallsBackToHerdrsContext(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"workspace_id":"w9Q","tab_id":"w9Q:t1","focused_pane_id":"w9Q:p1"}`)
	for _, key := range []string{"ARRANGE_PANE", "ARRANGE_TAB", "ARRANGE_WS", "ARRANGE_MODE"} {
		t.Setenv(key, "")
	}

	got, err := resolveTarget()
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}
	if want := (target{PaneID: "w9Q:p1", TabID: "w9Q:t1", WorkspaceID: "w9Q"}); got != want {
		t.Errorf("target = %+v, want %+v", got, want)
	}
}

func TestResolveTargetWithNothingToGoOn(t *testing.T) {
	for _, key := range []string{"ARRANGE_PANE", "ARRANGE_TAB", "ARRANGE_WS", "HERDR_PLUGIN_CONTEXT_JSON"} {
		t.Setenv(key, "")
	}
	if _, err := resolveTarget(); !errors.Is(err, errNoPane) {
		t.Errorf("resolveTarget = %v, want errNoPane", err)
	}

	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", "{not json")
	if _, err := resolveTarget(); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("resolveTarget = %v, want a JSON complaint", err)
	}
}
