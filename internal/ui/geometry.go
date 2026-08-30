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
//
// It is never smaller than layout mode's panel, because `t` switches views inside
// the same popup and keeps whatever size herdr gave us — a popup opened on a
// two-row tree should still be able to show the layout help.
func TreePopupSize(s *herdr.SessionSnapshot, paneID, tabID, workspaceID string) (width, height int) {
	width, floor := LayoutPopupSize()
	height = len(buildRows(s, paneID, tabID, workspaceID)) + treeChrome + popupBorderHeight
	return width, min(max(height, floor), treePopupMaxHeight)
}
