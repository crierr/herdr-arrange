package herdr

import "context"

// Typed wrappers for the handful of methods this plugin needs. Each result
// object is `{"type": ..., <field>: ...}`; the anonymous structs below name the
// one field we care about.

// Snapshot returns the entire session: workspaces, tabs and panes in one call.
func (c *Client) Snapshot(ctx context.Context) (*SessionSnapshot, error) {
	var out struct {
		Snapshot SessionSnapshot `json:"snapshot"`
	}
	if err := c.Call(ctx, "session.snapshot", nil, &out); err != nil {
		return nil, err
	}
	return &out.Snapshot, nil
}

// ExportLayout returns the split tree of a tab. An empty tabID means the
// focused tab.
func (c *Client) ExportLayout(ctx context.Context, tabID string) (*LayoutDescription, error) {
	params := map[string]any{}
	if tabID != "" {
		params["tab_id"] = tabID
	}
	var out struct {
		Layout LayoutDescription `json:"layout"`
	}
	if err := c.Call(ctx, "layout.export", params, &out); err != nil {
		return nil, err
	}
	return &out.Layout, nil
}

// ExportLayoutForPane returns the split tree of the tab containing paneID.
func (c *Client) ExportLayoutForPane(ctx context.Context, paneID string) (*LayoutDescription, error) {
	var out struct {
		Layout LayoutDescription `json:"layout"`
	}
	params := map[string]any{"pane_id": paneID}
	if err := c.Call(ctx, "layout.export", params, &out); err != nil {
		return nil, err
	}
	return &out.Layout, nil
}

// SetSplitRatio sets the ratio of one split, addressed by a path of booleans
// from the root (false = first child, true = second).
//
// herdr clamps the ratio to [0.1, 0.9].
func (c *Client) SetSplitRatio(ctx context.Context, tabID string, path []bool, ratio float64) error {
	if path == nil {
		path = []bool{}
	}
	params := map[string]any{"path": path, "ratio": ratio}
	if tabID != "" {
		params["tab_id"] = tabID
	}
	return c.Call(ctx, "layout.set_split_ratio", params, nil)
}

// SwapDirection swaps a pane with its neighbour in a direction. It preserves
// split shape, ratios, pane ids and running processes, but cannot change the
// shape of the tree. Focus follows the moved pane.
func (c *Client) SwapDirection(ctx context.Context, paneID string, dir Direction) (*SwapResult, error) {
	var out struct {
		Swap SwapResult `json:"swap"`
	}
	params := map[string]any{"pane_id": paneID, "direction": string(dir)}
	if err := c.Call(ctx, "pane.swap", params, &out); err != nil {
		return nil, err
	}
	return &out.Swap, nil
}

// SwapPanes exchanges two named panes in the same tab.
func (c *Client) SwapPanes(ctx context.Context, sourcePaneID, targetPaneID string) (*SwapResult, error) {
	var out struct {
		Swap SwapResult `json:"swap"`
	}
	params := map[string]any{"source_pane_id": sourcePaneID, "target_pane_id": targetPaneID}
	if err := c.Call(ctx, "pane.swap", params, &out); err != nil {
		return nil, err
	}
	return &out.Swap, nil
}

// MovePane moves a pane to another tab or workspace, preserving its pane id
// (within a workspace), terminal and running process. Moving a pane into its
// own tab is refused with Reason "same_tab"; use SwapPanes instead.
func (c *Client) MovePane(ctx context.Context, paneID string, dest Destination, focus bool) (*MoveResult, error) {
	var out struct {
		Move MoveResult `json:"move_result"`
	}
	params := map[string]any{"pane_id": paneID, "destination": dest, "focus": focus}
	if err := c.Call(ctx, "pane.move", params, &out); err != nil {
		return nil, err
	}
	return &out.Move, nil
}

// FocusPane focuses a pane, and by extension its tab and workspace.
func (c *Client) FocusPane(ctx context.Context, paneID string) error {
	return c.Call(ctx, "pane.focus", map[string]any{"pane_id": paneID}, nil)
}

// Zoom turns a pane's zoom on or off. mode is "toggle", "on" or "off".
func (c *Client) Zoom(ctx context.Context, paneID, mode string) (*ZoomResult, error) {
	var out struct {
		Zoom ZoomResult `json:"zoom"`
	}
	params := map[string]any{"pane_id": paneID, "mode": mode}
	if err := c.Call(ctx, "pane.zoom", params, &out); err != nil {
		return nil, err
	}
	return &out.Zoom, nil
}

// PopupOptions configures OpenPopup.
type PopupOptions struct {
	PluginID   string
	Entrypoint string
	Width      *PopupSize
	Height     *PopupSize
	Env        map[string]string
	Focus      bool
}

// OpenPopup opens a session-modal popup pane running one of the plugin's
// declared [[panes]] entrypoints. Only one popup can be open at a time; the
// call fails with code "ui_busy" otherwise.
//
// A popup is not a pane: it has no pane id, does not appear in pane listings,
// and receives every keystroke ahead of herdr's own keybindings.
func (c *Client) OpenPopup(ctx context.Context, opts PopupOptions) error {
	params := map[string]any{
		"plugin_id":  opts.PluginID,
		"entrypoint": opts.Entrypoint,
		"placement":  "popup",
		"focus":      opts.Focus,
	}
	if opts.Width != nil {
		params["width"] = *opts.Width
	}
	if opts.Height != nil {
		params["height"] = *opts.Height
	}
	if len(opts.Env) > 0 {
		params["env"] = opts.Env
	}
	return c.Call(ctx, "plugin.pane.open", params, nil)
}

// Notify shows a toast. Used to surface failures from the action processes,
// which have no UI of their own.
func (c *Client) Notify(ctx context.Context, title, body string) error {
	params := map[string]any{"title": title}
	if body != "" {
		params["body"] = body
	}
	return c.Call(ctx, "notification.show", params, nil)
}
