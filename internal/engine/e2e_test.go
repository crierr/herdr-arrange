package engine

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/crierr/herdr-arrange/internal/herdr"
	"github.com/crierr/herdr-arrange/internal/tree"
)

// The end-to-end test drives a real herdr server, so it is opt-in: it needs a
// throwaway workspace, because it will rearrange every pane in the workspace's
// first tab repeatedly.
//
//	herdr workspace create --label arrange-e2e --no-focus
//	# split its root pane a few times, then:
//	ARRANGE_E2E=w1V go test ./internal/engine/ -run E2E -v
//
// Everything else in this package tests against the modelled server in
// fake_test.go. This exists to keep that model honest.
func e2eWorkspace(t *testing.T) (*herdr.Client, string) {
	t.Helper()
	ws := os.Getenv("ARRANGE_E2E")
	if ws == "" {
		t.Skip("set ARRANGE_E2E=<throwaway workspace id> to run the end-to-end test")
	}
	client, err := herdr.New()
	if err != nil {
		t.Skipf("no herdr server: %v", err)
	}
	return client, ws
}

// TestE2EPresetsPreserveEveryTerminal is the claim the whole design rests on:
// rearranging a tab never destroys a pane's terminal, so whatever is running in
// it keeps running. layout.apply cannot do this, which is why the reconciler
// takes the long way round.
func TestE2EPresetsPreserveEveryTerminal(t *testing.T) {
	client, ws := e2eWorkspace(t)
	ctx := context.Background()

	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Work on the workspace's first tab.
	var tabID string
	for _, tab := range snapshot.Tabs {
		if tab.WorkspaceID == ws {
			tabID = tab.TabID
			break
		}
	}
	if tabID == "" {
		t.Fatalf("workspace %s has no tabs", ws)
	}

	terminals := map[string]string{} // pane id -> terminal id
	var pane string
	for _, p := range snapshot.Panes {
		if p.TabID == tabID {
			terminals[p.PaneID] = p.TerminalID
			if pane == "" {
				pane = p.PaneID
			}
		}
	}
	if len(terminals) < 3 {
		t.Fatalf("tab %s has %d panes; split it a few more times first", tabID, len(terminals))
	}
	t.Logf("arranging %d panes in %s, operating on %s", len(terminals), tabID, pane)

	eng := New(client, t.TempDir(), pane, tabID, ws)

	for _, preset := range tree.Presets {
		before, err := eng.Tab(ctx)
		if err != nil {
			t.Fatalf("%s: read tab: %v", preset.Name(), err)
		}
		want := preset.Build(before.Tree.Leaves(), pane)

		if err := eng.ApplyPreset(ctx, preset); err != nil && !errors.Is(err, tree.ErrNoChange) {
			t.Fatalf("%s: %v", preset.Name(), err)
		}

		after, err := eng.Tab(ctx)
		if err != nil {
			t.Fatalf("%s: re-read tab: %v", preset.Name(), err)
		}
		if !tree.Equal(after.Tree, want) {
			t.Fatalf("%s: tab is %#v, want %#v", preset.Name(), after.Tree, want)
		}
		if got, ok := tree.Detect(after.Tree); !ok || !tree.SameShape(after.Tree, got.Build(after.Tree.Leaves(), after.Tree.FirstLeaf())) {
			t.Errorf("%s: the applied layout is not detected back", preset.Name())
		}

		// No pane may have been recreated, and none left over in a scratch tab.
		snapshot, err = client.Snapshot(ctx)
		if err != nil {
			t.Fatalf("%s: snapshot: %v", preset.Name(), err)
		}
		seen := 0
		for _, p := range snapshot.Panes {
			if p.WorkspaceID != ws {
				continue
			}
			seen++
			if p.TabID != tabID {
				t.Errorf("%s: pane %s is in %s, not %s", preset.Name(), p.PaneID, p.TabID, tabID)
			}
			if want := terminals[p.PaneID]; want != "" && p.TerminalID != want {
				t.Errorf("%s: pane %s has terminal %s, want %s: the process was killed",
					preset.Name(), p.PaneID, p.TerminalID, want)
			}
		}
		if seen != len(terminals) {
			t.Fatalf("%s: workspace holds %d panes, started with %d", preset.Name(), seen, len(terminals))
		}
		t.Logf("%-16s ok  %s", preset.Name(), after.Tree)
	}

	// Nothing should be left for the startup drain to do.
	if journal, err := loadJournal(os.Getenv("HERDR_PLUGIN_STATE_DIR")); err == nil && journal != nil {
		t.Errorf("a parking journal was left behind: %+v", journal)
	}
}

// TestE2EReSplitWalksUp exercises the shift-key operation against the real
// server, including its no-op at the tab edge.
func TestE2EReSplitWalksUp(t *testing.T) {
	client, ws := e2eWorkspace(t)
	ctx := context.Background()

	snapshot, err := client.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var tabID, pane string
	for _, tab := range snapshot.Tabs {
		if tab.WorkspaceID == ws {
			tabID = tab.TabID
			break
		}
	}
	for _, p := range snapshot.Panes {
		if p.TabID == tabID {
			pane = p.PaneID
		}
	}

	eng := New(client, t.TempDir(), pane, tabID, ws)
	for press := 1; press <= 4; press++ {
		before, err := eng.Tab(ctx)
		if err != nil {
			t.Fatalf("press %d: %v", press, err)
		}
		err = eng.ReSplit(ctx, herdr.Left)
		if errors.Is(err, tree.ErrNoChange) {
			t.Logf("press %d: at the tab edge, nothing to do", press)
			break
		}
		if err != nil {
			t.Fatalf("press %d: %v", press, err)
		}
		after, err := eng.Tab(ctx)
		if err != nil {
			t.Fatalf("press %d: %v", press, err)
		}
		expected, err := tree.ReSplit(before.Tree, pane, herdr.Left)
		if err != nil {
			t.Fatalf("press %d: %v", press, err)
		}
		if !tree.Equal(after.Tree, expected) {
			t.Fatalf("press %d: tab is %#v, want %#v", press, after.Tree, expected)
		}
		t.Logf("press %d: %s", press, after.Tree)
	}
}
