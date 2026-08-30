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

	// treePopupMaxHeight caps tree mode when the snapshot does not say how much
	// screen there is: a session with fifty panes would otherwise ask for a popup
	// taller than the terminal. Past the cap the viewport scrolls, which it is
	// built to do.
	treePopupMaxHeight = 30

	// treePopupScreenMargin is how much of the screen tree mode leaves alone when
	// it does know the size. A popup the full height of the terminal reads as the
	// plugin having taken the session over rather than floating above it.
	treePopupScreenMargin = 4
)

// LayoutPopupSize is the outer popup size layout mode's help panel needs.
func LayoutPopupSize() (width, height int) {
	return layoutPanelWidth + popupBorderWidth, layoutPanelHeight + popupBorderHeight
}

// TreePopupSize is the outer popup size for tree mode: tall enough for the whole
// session, so the common case needs no scrolling.
//
// Every row counts, including the panes the tree does not show until it is unfolded.
// The popup cannot be resized without closing it, so sizing it for the deepest fold
// level is what makes unfolding free.
func TreePopupSize(s *herdr.SessionSnapshot, paneID, tabID, workspaceID string) (width, height int) {
	return treeSizeForRows(len(buildRows(s, paneID, tabID, workspaceID)), popupRoom(s))
}

// popupRoom is how tall a popup herdr has room for, taken from the area it draws
// tabs in — which is the same area it fits a popup into. Zero when the snapshot
// does not say, which is the only reason the blind cap still exists.
func popupRoom(s *herdr.SessionSnapshot) int {
	room := 0
	for _, l := range s.Layouts {
		room = max(room, l.Area.Height)
	}
	return room
}

// treeSizeForRows is the outer popup size a tree of n rows wants, given how much
// screen there is. The UI needs this too: switching to tree mode reopens the popup
// when the tree does not fit the one we are in, and by then the rows are all it has.
//
// It is never smaller than layout mode's panel. Reopening the popup is the only way
// to resize it, and that costs a flicker, so a session whose tree fits inside the
// layout panel is better off asking for the same size and switching views for free.
func treeSizeForRows(n, room int) (width, height int) {
	width, floor := LayoutPopupSize()

	ceiling := treePopupMaxHeight
	if room > 0 {
		ceiling = room - treePopupScreenMargin
	}
	height = n + treeChrome + popupBorderHeight
	return width, max(min(height, ceiling), floor)
}
