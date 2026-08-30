// Command arrange is the herdr-arrange plugin: an interactive popup UI for
// moving, swapping, re-splitting and laying out herdr panes.
//
// One binary, several roles, chosen by the first argument:
//
//	arrange open        [[actions]]  open the popup, layout mode if the tab has >1 pane
//	arrange open-tree   [[actions]]  open the popup in tree-view mode
//	arrange ui          [[panes]]    the popup UI itself
//	arrange drain       [[startup]]  recover panes stranded by an interrupted rebuild
//	arrange call M [P]               debug: send one raw API call and print the result
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/crierr/herdr-arrange/internal/engine"
	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/ui"
)

const usage = `arrange — interactive pane move, swap, re-split and layout

usage:
  arrange open              open the arrange popup
  arrange open-tree         open the popup in tree-view mode
  arrange ui                run the popup UI (invoked by herdr)
  arrange drain             recover panes from an interrupted rebuild
  arrange call M [params]   send one raw API call and print the result
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch cmd := os.Args[1]; cmd {
	case "call":
		err = runCall(os.Args[2:])
	case "ui":
		err = runUI()
	case "open", "open-tree", "drain":
		err = fmt.Errorf("%s: not implemented yet", cmd)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "arrange: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "arrange: %v\n", err)
		os.Exit(1)
	}
}

// target names the pane the popup arranges, and which view to open on.
type target struct {
	Mode        ui.Mode
	PaneID      string
	TabID       string
	WorkspaceID string
}

// pluginContext is the part of HERDR_PLUGIN_CONTEXT_JSON we need. A popup is not
// a pane, so it gets no HERDR_PANE_ID; the pane being arranged is whichever one
// was focused when the action ran.
type pluginContext struct {
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	FocusedPaneID string `json:"focused_pane_id"`
}

// resolveTarget works out which pane to arrange. The action passes it explicitly
// in the popup's environment; falling back to herdr's own context is what makes
// `arrange ui` runnable by hand during development.
func resolveTarget() (target, error) {
	t := target{
		PaneID:      os.Getenv("ARRANGE_PANE"),
		TabID:       os.Getenv("ARRANGE_TAB"),
		WorkspaceID: os.Getenv("ARRANGE_WS"),
	}
	if os.Getenv("ARRANGE_MODE") == "tree" {
		t.Mode = ui.ModeTree
	}

	if raw := os.Getenv("HERDR_PLUGIN_CONTEXT_JSON"); raw != "" {
		var ctx pluginContext
		if err := json.Unmarshal([]byte(raw), &ctx); err != nil {
			return t, fmt.Errorf("HERDR_PLUGIN_CONTEXT_JSON is not valid JSON: %w", err)
		}
		if t.PaneID == "" {
			t.PaneID = ctx.FocusedPaneID
		}
		if t.TabID == "" {
			t.TabID = ctx.TabID
		}
		if t.WorkspaceID == "" {
			t.WorkspaceID = ctx.WorkspaceID
		}
	}

	if t.PaneID == "" {
		return t, errors.New("no pane to arrange: set ARRANGE_PANE, or run this from a herdr plugin action")
	}
	return t, nil
}

// runUI is the popup itself.
func runUI() error {
	t, err := resolveTarget()
	if err != nil {
		return err
	}
	client, err := herdr.New()
	if err != nil {
		return err
	}
	// An unset state directory only disables the parking journal, so a hand-run
	// UI still works — it just has no crash recovery.
	eng := engine.New(client, os.Getenv("HERDR_PLUGIN_STATE_DIR"), t.PaneID, t.TabID, t.WorkspaceID)
	return ui.Run(eng, t.Mode)
}

// runCall is a development aid: it sends one arbitrary method with JSON params
// and pretty-prints the result, so the API can be poked without a UI.
func runCall(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("call: want a method name")
	}
	client, err := herdr.New()
	if err != nil {
		return err
	}

	var params any
	if len(args) > 1 && args[1] != "" {
		if err := json.Unmarshal([]byte(args[1]), &params); err != nil {
			return fmt.Errorf("call: params is not valid JSON: %w", err)
		}
	}

	var result json.RawMessage
	if err := client.Call(context.Background(), args[0], params, &result); err != nil {
		return err
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, result, "", "  "); err != nil {
		fmt.Println(string(result))
		return nil
	}
	fmt.Println(pretty.String())
	return nil
}
