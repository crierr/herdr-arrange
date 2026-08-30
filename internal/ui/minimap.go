package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/crierr/herdr-arrange/internal/herdr"
)

// The minimap's size, in cells of the popup.
//
// Its height is fixed because the popup's has to be: the action asks herdr for a
// popup before anything is known about the tab, and herdr cannot resize one, so a
// map whose height followed the layout would not fit the popup it is drawn in. Its
// width follows the tab's proportions, which costs nothing to vary.
const (
	minimapRows = 7
	// minimapHeight is the whole block: the drawing, and the blank line that keeps
	// it off the help below it.
	minimapHeight = minimapRows + 1

	minimapMinWidth = 12
	minimapMaxWidth = layoutPanelWidth - 4
)

// minimapBlock is the picture of the current tab, padded to exactly minimapHeight
// lines so the help under it never moves as tabs and layouts change.
func (m Model) minimapBlock() []string {
	lines := m.minimap()
	for len(lines) < minimapHeight {
		lines = append(lines, "")
	}
	return lines[:minimapHeight]
}

// minimap draws the tab as one box per pane, with the pane being arranged named and
// measured inside its own — which is the one thing a picture cannot say for itself.
func (m Model) minimap() []string {
	t := m.theme
	switch {
	case m.geoErr != nil:
		// The popup can still rearrange the tab without the picture, so this belongs
		// here rather than in the status line, where it would overwrite whatever the
		// last action reported and do so after every reload.
		return []string{" " + t.dim.Render(truncate("no map: "+m.geoErr.Error(), m.width-1))}
	case m.geo == nil || len(m.geo.Panes) == 0:
		return []string{" " + t.dim.Render("reading the tab's layout…")}
	}

	area := m.geo.Area
	width, height := minimapSize(area)
	c := newCanvas(width, height)

	// The panes tile the area, so their outer edges are the tab's own border; it is
	// drawn anyway, so a herdr that ever reports a pane short still gets a frame.
	c.box(span{0, 0, width - 1, height - 1})

	for _, p := range m.geo.Panes {
		box := span{
			x1: scale(p.Rect.X, area.X, area.Width, width),
			y1: scale(p.Rect.Y, area.Y, area.Height, height),
			x2: scale(p.Rect.X+p.Rect.Width, area.X, area.Width, width),
			y2: scale(p.Rect.Y+p.Rect.Height, area.Y, area.Height, height),
		}
		c.box(box)
		if p.PaneID == m.eng.PaneID() {
			c.label(box, labelFor(p, box))
		}
	}
	return c.render(t)
}

// minimapSize is the drawing's size: as tall as minimapRows, and as wide as the
// tab's own proportions make it, so a wide tab draws wide and a tall one does not
// pretend to be. A map cell is the same shape as a screen cell, so the aspect ratio
// carries over by scaling both axes by the same amount.
func minimapSize(area herdr.LayoutRect) (width, height int) {
	if area.Width <= 0 || area.Height <= 0 {
		return minimapMaxWidth, minimapRows
	}
	width = int(math.Round(minimapRows * float64(area.Width) / float64(area.Height)))
	return min(max(width, minimapMinWidth), minimapMaxWidth), minimapRows
}

// scale puts a screen coordinate on the map, as a fraction of the way across the
// area. Coordinates are scaled rather than sizes, so two panes sharing a border on
// screen share a line on the map: rounding can squash a pane flat, but it can never
// open a gap between two of them or make them overlap.
func scale(v, origin, span, cells int) int {
	if span <= 0 || cells < 2 {
		return 0
	}
	return int(math.Round(float64(v-origin) * float64(cells-1) / float64(span)))
}

// labelFor is what to write inside a pane's box: its id and its size in cells.
//
// The candidates are tried in falling order of how much they say, and if none of
// them fits, the box is left empty — a box with "w1S…" in it says less than a box
// with nothing in it.
func labelFor(p herdr.LayoutPane, box span) []string {
	id, short := p.PaneID, shortID(p.PaneID)
	size := fmt.Sprintf("%dx%d", p.Rect.Width, p.Rect.Height)
	width, height := box.interior()

	for _, lines := range [][]string{
		{id, size},
		{short, size},
		{id + " " + size},
		{short + " " + size},
		{short},
	} {
		if len(lines) > height {
			continue
		}
		fits := true
		for _, line := range lines {
			fits = fits && len([]rune(line)) <= width
		}
		if fits {
			return lines
		}
	}
	return nil
}

// span is a box on the canvas, by the cells its corners sit in.
type span struct{ x1, y1, x2, y2 int }

// interior is the room inside a box, border excluded.
func (s span) interior() (width, height int) {
	return s.x2 - s.x1 - 1, s.y2 - s.y1 - 1
}

// edge is a side of a canvas cell that a line leaves by.
type edge uint8

const (
	edgeN edge = 1 << iota
	edgeS
	edgeE
	edgeW
)

// boxRunes maps a set of edges to the box-drawing rune with exactly those arms. A
// stub with a single arm borrows the rune of the line it is part of.
var boxRunes = map[edge]rune{
	edgeN:                         '│',
	edgeS:                         '│',
	edgeN | edgeS:                 '│',
	edgeE:                         '─',
	edgeW:                         '─',
	edgeE | edgeW:                 '─',
	edgeS | edgeE:                 '┌',
	edgeS | edgeW:                 '┐',
	edgeN | edgeE:                 '└',
	edgeN | edgeW:                 '┘',
	edgeN | edgeS | edgeE:         '├',
	edgeN | edgeS | edgeW:         '┤',
	edgeS | edgeE | edgeW:         '┬',
	edgeN | edgeE | edgeW:         '┴',
	edgeN | edgeS | edgeE | edgeW: '┼',
}

// canvas is a grid of edges with text written over it.
//
// Boxes are accumulated as edges per cell rather than written straight out as
// runes, because that is what makes a junction come out as ├ or ┼ instead of as
// whichever pane happened to be drawn there last.
type canvas struct {
	width, height int
	edges         []edge

	// text is what is written inside the boxes, and spans where on each line it
	// sits, so the label can be styled apart from the frame around it.
	text  [][]rune
	spans map[int][2]int
}

func newCanvas(width, height int) *canvas {
	c := &canvas{
		width:  width,
		height: height,
		edges:  make([]edge, width*height),
		text:   make([][]rune, height),
		spans:  map[int][2]int{},
	}
	for y := range c.text {
		c.text[y] = []rune(strings.Repeat(" ", width))
	}
	return c
}

// add records that a line leaves a cell by a side. Out-of-range cells are dropped
// rather than clamped: a box that does not fit is better cut off than folded back
// over the map.
func (c *canvas) add(x, y int, e edge) {
	if x < 0 || y < 0 || x >= c.width || y >= c.height {
		return
	}
	c.edges[y*c.width+x] |= e
}

// box draws a rectangle. Each segment is recorded from both of the cells it joins,
// so the arms of a junction are there however the boxes around it are ordered.
func (c *canvas) box(s span) {
	for x := s.x1; x < s.x2; x++ {
		for _, y := range [2]int{s.y1, s.y2} {
			c.add(x, y, edgeE)
			c.add(x+1, y, edgeW)
		}
	}
	for y := s.y1; y < s.y2; y++ {
		for _, x := range [2]int{s.x1, s.x2} {
			c.add(x, y, edgeS)
			c.add(x, y+1, edgeN)
		}
	}
}

// label writes lines inside a box, centred. Nothing is written that would land on
// the border: labelFor has already decided what fits, and this is what holds it to
// it.
func (c *canvas) label(s span, lines []string) {
	width, height := s.interior()
	top := s.y1 + 1 + max(height-len(lines), 0)/2
	for i, line := range lines {
		y, runes := top+i, []rune(line)
		if y <= s.y1 || y >= s.y2 || y >= c.height || len(runes) > width {
			continue
		}
		x := s.x1 + 1 + max(width-len(runes), 0)/2
		copy(c.text[y][x:], runes)
		c.spans[y] = [2]int{x, x + len(runes)}
	}
}

// render draws the canvas: the frame as furniture, the label as the one thing on it
// worth reading.
func (c *canvas) render(t theme) []string {
	// An empty string still comes back wrapped in escape codes, which is a line the
	// terminal has to think about for nothing.
	paint := func(style lipgloss.Style, s string) string {
		if s == "" {
			return ""
		}
		return style.Render(s)
	}

	out := make([]string, c.height)
	for y := range c.height {
		row := make([]rune, c.width)
		for x := range c.width {
			row[x] = c.text[y][x]
			if e := c.edges[y*c.width+x]; e != 0 && row[x] == ' ' {
				row[x] = boxRunes[e]
			}
		}

		line := paint(t.rule, string(row))
		if at, ok := c.spans[y]; ok {
			line = paint(t.rule, string(row[:at[0]])) +
				paint(t.active, string(row[at[0]:at[1]])) +
				paint(t.rule, string(row[at[1]:]))
		}
		out[y] = " " + line
	}
	return out
}
