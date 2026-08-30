package ui

import "github.com/crierr/herdr-arrange/internal/herdr"

// The popup's frame. herdr gives plugin.pane.open an *outer* size and runs the
// program in what is left after the border: two rows and — because it keeps a
// column for the scrollbar gutter — three columns (`resolve_popup_geometry`).
// The views state the size they need to render; these turn that into the size the
// action has to ask for.
const (
	popupBorderWidth  = 3
	popupBorderHeight = 2

	// treePopupMaxHeight caps tree mode. Nothing in the API reports the terminal
	// size, so the action cannot ask for "the smaller of the tree and 80% of the
	// screen"; a session with fifty panes would otherwise ask for a popup as tall
	// as the terminal. Past this the viewport scrolls, which it is built to do.
	treePopupMaxHeight = 30
)

// LayoutPopupSize is the outer popup size layout mode's help panel needs.
func LayoutPopupSize() (width, height int) {
	return layoutPanelWidth + popupBorderWidth, layoutPanelHeight + popupBorderHeight
}

// TreePopupSize is the outer popup size for tree mode: tall enough for the whole
// session, so the common case needs no scrolling.
func TreePopupSize(s *herdr.SessionSnapshot, paneID, tabID, workspaceID string) (width, height int) {
	return treeSizeForRows(len(buildRows(s, paneID, tabID, workspaceID)))
}

// treeSizeForRows is the outer popup size a tree of n rows wants. The UI needs this
// too: switching to tree mode reopens the popup when the tree does not fit the one
// we are in, and by then the rows are all it has.
//
// It is never smaller than layout mode's panel. Reopening the popup is the only way
// to resize it, and that costs a flicker, so a session whose tree fits inside the
// layout panel is better off asking for the same size and switching views for free.
func treeSizeForRows(n int) (width, height int) {
	width, floor := LayoutPopupSize()
	height = n + treeChrome + popupBorderHeight
	return width, min(max(height, floor), treePopupMaxHeight)
}
