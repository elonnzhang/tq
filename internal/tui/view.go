// Package tui shows drawn output in a full-screen viewer that scrolls both
// ways, for output too wide or too long for the terminal.
package tui

import (
	"fmt"
	"strings"
)

// Draw draws the output to show, at most width cells wide, or at its
// natural width if width is zero. It returns the lines and how many of them
// at the top are a header to keep in view while scrolling.
type Draw func(width int) (lines []string, head int)

// Scroll bar characters: a thumb on a thin rail. The horizontal thumb is
// half a cell tall, about as thick as the vertical one is wide, since cells
// are about twice as tall as they are wide.
const (
	vThumb = "█"
	vTrack = "▕"
	hThumb = "▀"
	hTrack = "▔"
)

// Scroll bar colors, as SGR codes, when colors are on.
const (
	thumbColor = "96" // bright cyan
	trackColor = "90" // gray
)

// How far the arrow keys and the mouse wheel scroll.
const (
	hscroll    = 8 // cells for an arrow key
	wheelLines = 3 // lines for a turn of the wheel
	wheelCells = 4 // cells for a turn of the wheel sideways
)

// viewer is what the viewer shows and where it is scrolled to.
type viewer struct {
	draw  Draw
	fit   bool // whether the output is drawn to fit the screen width
	color bool // whether to color the scroll bars
	w, h  int  // the screen size

	lines  []string
	head   int // header lines, kept at the top if the screen has room
	widest int // the width of the widest line

	top  int // the first line shown below the header
	left int // the first cell shown of each line

	dragging int // which scroll bar's thumb is being dragged, if any
	grab     int // where in the thumb it is held

	notice string // shown in the status line for a while, if set
}

// Scroll bars, for dragging.
const (
	noBar = iota
	vertical
	horizontal
)

// resize sets the screen size, drawing the output again if it is to fit.
func (v *viewer) resize(w, h int) {
	changed := w != v.w
	v.w, v.h = max(w, 1), max(h, 1)
	if v.lines == nil || v.fit && changed {
		v.redraw()
	}
	v.clamp()
}

// redraw draws the output for the current screen width.
func (v *viewer) redraw() {
	limit := 0
	if v.fit {
		limit = max(v.w-1, 1) // leaving a column for the scroll bar
	}
	v.lines, v.head = v.draw(limit)
	v.widest = 0
	for _, l := range v.lines {
		v.widest = max(v.widest, width(l))
	}
}

// layout works out what the screen shows: how many header lines stay at
// the top, how many rows and cells the lines under them get, and which
// scroll bars there are.
func (v *viewer) layout() (head, rows, cols int, vbar, hbar bool) {
	if v.head*2 <= v.h-2 { // the header takes at most half the screen
		head = v.head
	}
	// One scroll bar takes room that may call for the other; twice
	// settles it.
	for range 2 {
		cols = v.w
		if vbar {
			cols--
		}
		hbar = v.widest > cols
		rows = v.h - 1 - head
		if hbar {
			rows--
		}
		vbar = len(v.lines)-head > rows
	}
	return head, max(rows, 1), max(cols, 1), vbar, hbar
}

// clamp keeps the view within the output.
func (v *viewer) clamp() {
	head, rows, cols, _, _ := v.layout()
	v.top = max(0, min(v.top, len(v.lines)-head-rows))
	v.left = max(0, min(v.left, v.widest-cols))
}

// frame returns the rows of the screen.
func (v *viewer) frame() []string {
	head, rows, cols, vbar, hbar := v.layout()
	out := make([]string, 0, v.h)
	row := func(line, bar string) string {
		if vbar {
			return slice(line, v.left, cols) + bar
		}
		return slice(line, v.left, cols)
	}
	for _, l := range v.lines[:head] {
		out = append(out, row(l, " "))
	}
	pos, size := thumb(rows, rows, len(v.lines)-head, v.top)
	for i := range rows {
		line := ""
		if k := head + v.top + i; k < len(v.lines) {
			line = v.lines[k]
		}
		bar := v.paint(trackColor, vTrack)
		if i >= pos && i < pos+size {
			bar = v.paint(thumbColor, vThumb)
		}
		out = append(out, row(line, bar))
	}
	if hbar {
		pos, size := thumb(cols, cols, v.widest, v.left)
		bar := v.paint(trackColor, strings.Repeat(hTrack, pos)) +
			v.paint(thumbColor, strings.Repeat(hThumb, size)) +
			v.paint(trackColor, strings.Repeat(hTrack, cols-pos-size))
		if vbar {
			bar += " "
		}
		out = append(out, bar)
	}
	return append(out, v.status(head, rows, cols))
}

// paint colors s if colors are on.
func (v *viewer) paint(color, s string) string {
	if !v.color || s == "" {
		return s
	}
	return "\x1b[" + color + "m" + s + "\x1b[0m"
}

// status returns the status line: where the view is, and the keys or the
// notice if there is one.
func (v *viewer) status(head, rows, cols int) string {
	n := len(v.lines) - head
	pos := fmt.Sprintf(" line %d-%d/%d", min(v.top+1, n), min(v.top+rows, n), n)
	if v.widest > cols {
		pos += fmt.Sprintf("  col %d-%d/%d", v.left+1, min(v.left+cols, v.widest), v.widest)
	}
	mode := "w: fit width"
	if v.fit {
		mode = "w: full width"
	}
	// The keys, or as many of them as there is room for.
	helps := []string{
		"←↓↑→/hjkl  space/b  g/G  0/$  " + mode + "  c: copy  q: quit ",
		mode + "  c: copy  q: quit ",
		"q: quit ",
	}
	if v.notice != "" {
		// A notice goes in front of all else.
		if width(pos)+width(v.notice)+2 > v.w {
			pos = ""
		}
		helps = []string{" " + v.notice + " "}
	}
	for _, help := range helps {
		if gap := v.w - width(pos) - width(help); gap >= 0 {
			pos += strings.Repeat(" ", gap) + help
			break
		}
	}
	return "\x1b[7m" + slice(pos, 0, v.w) + "\x1b[0m"
}

// thumb returns where the thumb goes on a scroll bar track cells long, for
// a view of shown of total items from first on: its first cell and length.
func thumb(track, shown, total, first int) (pos, size int) {
	if total <= shown {
		return 0, track
	}
	size = max(1, track*shown/total)
	pos = ((track-size)*first + (total-shown)/2) / (total - shown)
	return pos, size
}

// key acts on the key named k, as Bubble Tea names keys, and reports
// whether it quits the viewer.
func (v *viewer) key(k string) (quit bool) {
	_, rows, cols, _, _ := v.layout()
	switch k {
	case "q", "Q", "esc", "ctrl+c":
		return true
	case "j", "down", "enter", "ctrl+n", "ctrl+e":
		v.top++
	case "k", "up", "ctrl+p", "ctrl+y":
		v.top--
	case "space", "f", "pgdown", "ctrl+f":
		v.top += rows
	case "b", "pgup", "ctrl+b":
		v.top -= rows
	case "d", "ctrl+d":
		v.top += max(rows/2, 1)
	case "u", "ctrl+u":
		v.top -= max(rows/2, 1)
	case "g", "home":
		v.top = 0
	case "G", "end":
		v.top = len(v.lines)
	case "l", "right":
		v.left += hscroll
	case "h", "left":
		v.left -= hscroll
	case "L", "shift+right":
		v.left += max(cols/2, 1)
	case "H", "shift+left":
		v.left -= max(cols/2, 1)
	case "0":
		v.left = 0
	case "$":
		v.left = v.widest
	case "w":
		// Stay about as far down in the output drawn anew.
		n := len(v.lines)
		v.fit = !v.fit
		v.redraw()
		v.top = v.top * len(v.lines) / max(n, 1)
		v.left = 0
	}
	v.clamp()
	return false
}

// scroll moves the view down dy lines and right dx cells.
func (v *viewer) scroll(dy, dx int) {
	v.top += dy
	v.left += dx
	v.clamp()
}

// press handles the left mouse button going down at cell x of row y of the
// screen. On a scroll bar it moves the view there and picks up the thumb.
func (v *viewer) press(x, y int) {
	head, rows, cols, vbar, hbar := v.layout()
	switch {
	case vbar && x == cols && y >= head && y < head+rows:
		v.dragging = vertical
		v.grab = grab(rows, rows, len(v.lines)-head, v.top, y-head)
	case hbar && y == head+rows && x < cols:
		v.dragging = horizontal
		v.grab = grab(cols, cols, v.widest, v.left, x)
	default:
		return
	}
	v.drag(x, y)
}

// drag handles the mouse moving to cell x of row y with the button down,
// moving the view along with the thumb being dragged.
func (v *viewer) drag(x, y int) {
	head, rows, cols, _, _ := v.layout()
	switch v.dragging {
	case vertical:
		v.top = unthumb(rows, rows, len(v.lines)-head, y-head-v.grab)
	case horizontal:
		v.left = unthumb(cols, cols, v.widest, x-v.grab)
	}
	v.clamp()
}

// release handles the mouse button going up.
func (v *viewer) release() {
	v.dragging = noBar
}

// grab returns where in its thumb a scroll bar laid out as thumb lays it
// out gets held when pressed at cell at: there if on the thumb, or else its
// middle, for the thumb to jump to the cell.
func grab(track, shown, total, first, at int) int {
	pos, size := thumb(track, shown, total, first)
	if at >= pos && at < pos+size {
		return at - pos
	}
	return size / 2
}

// unthumb returns the first item to show for the thumb of a scroll bar laid
// out as thumb lays it out to start at cell pos.
func unthumb(track, shown, total, pos int) int {
	_, size := thumb(track, shown, total, 0)
	if total <= shown || track <= size {
		return 0
	}
	pos = max(0, min(pos, track-size))
	return (pos*(total-shown) + (track-size)/2) / (track - size)
}

// text returns the output as drawn, without colors, for copying.
func (v *viewer) text() string {
	var b strings.Builder
	for _, l := range v.lines {
		b.WriteString(strings.TrimRight(plain(l), " "))
		b.WriteByte('\n')
	}
	return b.String()
}
