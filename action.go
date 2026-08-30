package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/crierr/herdr-arrange/internal/engine"
	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/ui"
)

// The plugin's own identity, as declared in herdr-plugin.toml. herdr passes the
// installed id in the environment, which is what we prefer; the constant is the
// fallback for running an action by hand.
const (
	defaultPluginID = "soh.arrange"
	uiEntrypointID  = "ui"
)

// An action is a short-lived process herdr spawns and reaps: a couple of socket
// calls, no user interaction. The timeouts only stop a wedged server from leaving
// the process around forever.
const (
	openTimeout  = 10 * time.Second
	drainTimeout = 60 * time.Second
)

// opener is the part of the API an open action uses.
type opener interface {
	Snapshot(ctx context.Context) (*herdr.SessionSnapshot, error)
	OpenPopup(ctx context.Context, opts herdr.PopupOptions) error
	Notify(ctx context.Context, title, body string) error
}

// runOpen is the [[actions]] side of the plugin.
func runOpen(mode ui.Mode) error {
	t, err := resolveTarget()
	if err != nil {
		return err
	}
	client, err := herdr.New()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	return open(ctx, client, t, mode)
}

// open decides which view to open on, asks herdr for a popup the right size for
// it, and hands the pane being arranged to the UI in the popup's environment.
//
// The size has to be decided out here: herdr sizes a popup when it opens it, and
// tree mode's natural height depends on how big the session is.
func open(ctx context.Context, client opener, t target, mode ui.Mode) error {
	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		return err
	}
	// A single-pane tab has no layout to arrange. The tree is the only view with
	// anything to offer there, so open on it rather than on a help panel whose keys
	// all decline.
	if mode == ui.ModeLayout && panesInTab(snapshot, t.TabID) < 2 {
		mode = ui.ModeTree
	}

	width, height := ui.LayoutPopupSize()
	if mode == ui.ModeTree {
		width, height = ui.TreePopupSize(snapshot, t.PaneID, t.TabID, t.WorkspaceID)
	}
	outerWidth, outerHeight := herdr.Cells(width), herdr.Cells(height)

	err = client.OpenPopup(ctx, herdr.PopupOptions{
		PluginID:   pluginID(),
		Entrypoint: uiEntrypointID,
		Width:      &outerWidth,
		Height:     &outerHeight,
		Focus:      true,
		Env: map[string]string{
			"ARRANGE_MODE": modeName(mode),
			"ARRANGE_PANE": t.PaneID,
			"ARRANGE_TAB":  t.TabID,
			"ARRANGE_WS":   t.WorkspaceID,
		},
	})
	if err != nil {
		return notifyOpenFailure(ctx, client, err)
	}
	return nil
}

// runDrain is the [[startup]] hook. A rebuild parks panes in a scratch tab and
// moves them back; if herdr is killed halfway through that, the panes are still
// alive but sitting somewhere the user did not put them. This brings them home on
// the next server start.
func runDrain() error {
	client, err := herdr.New()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), drainTimeout)
	defer cancel()

	moved, drainErr := engine.Drain(ctx, client, os.Getenv("HERDR_PLUGIN_STATE_DIR"))
	if moved > 0 {
		// Panes turning up in a tab nobody opened needs explaining, so say so even
		// though nothing went wrong this time round.
		body := fmt.Sprintf("recovered %s from an interrupted rebuild", panes(moved))
		if drainErr != nil {
			body = fmt.Sprintf("recovered %s of an interrupted rebuild, then failed: %v", panes(moved), drainErr)
		}
		_ = client.Notify(ctx, "Arrange", body)
		fmt.Println(body)
	}
	return drainErr
}

// notifyOpenFailure surfaces a refused popup. The action has no UI of its own and
// nothing is watching its exit code, so without a notification a keypress that
// cannot open the popup looks like a keybinding that does nothing at all. The
// error is still returned, which is what puts it in `herdr plugin log list`.
func notifyOpenFailure(ctx context.Context, client opener, err error) error {
	body := err.Error()
	switch {
	case herdr.Code(err) == "ui_busy":
		body = "close the current herdr dialog first"
	case strings.Contains(body, "popup already open"):
		body = "the arrange popup is already open"
	case strings.Contains(body, "too small"):
		body = "the terminal is too small for the arrange popup"
	}
	_ = client.Notify(ctx, "Arrange", body)
	return err
}

// pluginID is the id herdr installed us under, which is the id plugin.pane.open
// wants. It only differs from the manifest's when an action is run by hand.
func pluginID() string {
	if id := os.Getenv("HERDR_PLUGIN_ID"); id != "" {
		return id
	}
	return defaultPluginID
}

func modeName(mode ui.Mode) string {
	if mode == ui.ModeTree {
		return "tree"
	}
	return "layout"
}

// panesInTab counts a tab's panes. The snapshot's own PaneCount is per-tab too,
// but counting panes keeps this honest if the tab is not listed at all.
func panesInTab(s *herdr.SessionSnapshot, tabID string) int {
	n := 0
	for _, pane := range s.Panes {
		if pane.TabID == tabID {
			n++
		}
	}
	return n
}

func panes(n int) string {
	if n == 1 {
		return "1 pane"
	}
	return fmt.Sprintf("%d panes", n)
}

// errNoPane is returned when nothing in the environment says which pane to work
// on, which means the process was not started by herdr.
var errNoPane = errors.New("no pane to arrange: set ARRANGE_PANE, or run this from a herdr plugin action")
