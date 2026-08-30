package herdr

import (
	"encoding/json"
	"fmt"
)

// SplitDirection is the axis of a split: the new pane goes to the right of, or
// below, the existing one.
type SplitDirection string

const (
	Right SplitDirection = "right"
	Down  SplitDirection = "down"
)

// Direction is a compass direction, used for swaps and neighbour lookups.
type Direction string

const (
	Left  Direction = "left"
	DirRt Direction = "right"
	Up    Direction = "up"
	DirDn Direction = "down"
)

// LayoutDescription is the result of layout.export.
type LayoutDescription struct {
	WorkspaceID   string     `json:"workspace_id"`
	TabID         string     `json:"tab_id"`
	Zoomed        bool       `json:"zoomed"`
	FocusedPaneID string     `json:"focused_pane_id"`
	Root          LayoutNode `json:"root"`
}

// LayoutNode is one node of herdr's binary split tree. Exactly one of the
// pane fields or the split fields is meaningful, per Type.
type LayoutNode struct {
	Type string `json:"type"` // "pane" | "split"

	// Type == "pane".
	PaneID string            `json:"pane_id,omitempty"`
	Label  string            `json:"label,omitempty"`
	Cwd    string            `json:"cwd,omitempty"`
	Cmd    []string          `json:"command,omitempty"`
	Env    map[string]string `json:"env,omitempty"`

	// Type == "split".
	Direction SplitDirection `json:"direction,omitempty"`
	Ratio     float64        `json:"ratio,omitempty"`
	First     *LayoutNode    `json:"first,omitempty"`
	Second    *LayoutNode    `json:"second,omitempty"`
}

// IsPane reports whether n is a leaf.
func (n *LayoutNode) IsPane() bool { return n != nil && n.Type == "pane" }

// WorkspaceInfo describes one workspace.
type WorkspaceInfo struct {
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	PaneCount   int    `json:"pane_count"`
	TabCount    int    `json:"tab_count"`
	ActiveTabID string `json:"active_tab_id"`
}

// TabInfo describes one tab.
type TabInfo struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Number      int    `json:"number"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	PaneCount   int    `json:"pane_count"`
}

// PaneInfo describes one pane. Only the fields we use are modelled.
type PaneInfo struct {
	PaneID      string `json:"pane_id"`
	TerminalID  string `json:"terminal_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	Focused     bool   `json:"focused"`
	Label       string `json:"label,omitempty"`
	Title       string `json:"title,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	Agent       string `json:"agent,omitempty"`
	AgentStatus string `json:"agent_status,omitempty"`
}

// DisplayName is the best short human label for the pane.
func (p PaneInfo) DisplayName() string {
	for _, s := range []string{p.Label, p.Agent, p.Title} {
		if s != "" {
			return s
		}
	}
	return p.PaneID
}

// SessionSnapshot is the whole session in one response: every workspace, tab
// and pane. The tree view is built from a single call to session.snapshot.
type SessionSnapshot struct {
	Version            string          `json:"version"`
	Protocol           int             `json:"protocol"`
	Workspaces         []WorkspaceInfo `json:"workspaces"`
	Tabs               []TabInfo       `json:"tabs"`
	Panes              []PaneInfo      `json:"panes"`
	FocusedWorkspaceID string          `json:"focused_workspace_id,omitempty"`
	FocusedTabID       string          `json:"focused_tab_id,omitempty"`
	FocusedPaneID      string          `json:"focused_pane_id,omitempty"`
}

// SwapResult is the result of pane.swap. Reason is "no_neighbor",
// "same_pane", "not_found" or "cross_tab" when Changed is false.
type SwapResult struct {
	Changed       bool   `json:"changed"`
	SourcePaneID  string `json:"source_pane_id"`
	TargetPaneID  string `json:"target_pane_id,omitempty"`
	FocusedPaneID string `json:"focused_pane_id"`
	Reason        string `json:"reason,omitempty"`
}

// MoveResult is the result of pane.move. Reason is "same_tab" or
// "zoomed_tab" when Changed is false.
//
// Pane ids are workspace-scoped, so a cross-workspace move renames the pane:
// always re-read Pane.PaneID afterwards.
type MoveResult struct {
	Changed             bool           `json:"changed"`
	Pane                PaneInfo       `json:"pane"`
	PreviousPaneID      string         `json:"previous_pane_id"`
	PreviousTabID       string         `json:"previous_tab_id"`
	PreviousWorkspaceID string         `json:"previous_workspace_id"`
	FocusedPaneID       string         `json:"focused_pane_id"`
	CreatedTab          *TabInfo       `json:"created_tab,omitempty"`
	CreatedWorkspace    *WorkspaceInfo `json:"created_workspace,omitempty"`
	ClosedTabID         string         `json:"closed_tab_id,omitempty"`
	ClosedWorkspaceID   string         `json:"closed_workspace_id,omitempty"`
	Reason              string         `json:"reason,omitempty"`
}

// ZoomResult is the result of pane.zoom.
type ZoomResult struct {
	Changed       bool   `json:"changed"`
	ZoomChanged   bool   `json:"zoom_changed"`
	PaneID        string `json:"pane_id"`
	FocusedPaneID string `json:"focused_pane_id"`
	Zoomed        bool   `json:"zoomed"`
}

// Destination is a pane.move target. Use DestTab, DestNewTab or
// DestNewWorkspace to build one.
type Destination struct {
	Type string `json:"type"`

	// Type == "tab".
	TabID        string         `json:"tab_id,omitempty"`
	TargetPaneID string         `json:"target_pane_id,omitempty"`
	Split        SplitDirection `json:"split,omitempty"`
	Ratio        *float64       `json:"ratio,omitempty"`

	// Type == "new_tab" | "new_workspace".
	WorkspaceID string `json:"workspace_id,omitempty"`
	Label       string `json:"label,omitempty"`
	TabLabel    string `json:"tab_label,omitempty"`
}

// DestTab moves a pane into an existing tab, splitting targetPane (which
// becomes the `first` child) along split. A nil ratio lets herdr pick.
func DestTab(tabID, targetPaneID string, split SplitDirection, ratio *float64) Destination {
	return Destination{Type: "tab", TabID: tabID, TargetPaneID: targetPaneID, Split: split, Ratio: ratio}
}

// DestNewTab moves a pane into a fresh tab. An empty workspaceID means the
// pane's current workspace.
func DestNewTab(workspaceID, label string) Destination {
	return Destination{Type: "new_tab", WorkspaceID: workspaceID, Label: label}
}

// DestNewWorkspace moves a pane into a fresh workspace.
func DestNewWorkspace(label, tabLabel string) Destination {
	return Destination{Type: "new_workspace", Label: label, TabLabel: tabLabel}
}

// PopupSize is either a cell count or a percentage string like "80%".
type PopupSize struct {
	cells   int
	percent string
}

// Cells sizes a popup in terminal cells. Oversized values are clamped by herdr.
func Cells(n int) PopupSize { return PopupSize{cells: n} }

// Percent sizes a popup as a percentage (1..100) of the terminal area.
func Percent(n int) PopupSize { return PopupSize{percent: fmt.Sprintf("%d%%", n)} }

// MarshalJSON emits the bare integer or the percentage string herdr expects.
func (s PopupSize) MarshalJSON() ([]byte, error) {
	if s.percent != "" {
		return json.Marshal(s.percent)
	}
	return json.Marshal(s.cells)
}
