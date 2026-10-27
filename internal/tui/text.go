package tui

import (
	"strings"

	"github.com/clipperhouse/uax29/v2/graphemes"
	"github.com/mattn/go-runewidth"
)

// cond measures text in terminal cells the way package render does, with
// East Asian ambiguous characters one cell wide.
var cond = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

// walk calls text for each grapheme cluster of line with its width, and
// esc for each CSI escape sequence, such as a color, in between.
func walk(line string, text func(cluster string, width int) bool, esc func(seq string)) {
	for i := 0; i < len(line); {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			j = min(j+1, len(line))
			esc(line[i:j])
			i = j
			continue
		}
		seg := line[i:]
		if k := strings.IndexByte(seg, 0x1b); k > 0 {
			seg = seg[:k]
		}
		g := graphemes.FromString(seg)
		for g.Next() {
			if !text(g.Value(), cond.StringWidth(g.Value())) {
				return
			}
		}
		i += len(seg)
	}
}

// plain returns line without its escape sequences.
func plain(line string) string {
	var b strings.Builder
	walk(line, func(cluster string, _ int) bool {
		b.WriteString(cluster)
		return true
	}, func(string) {})
	return b.String()
}

// width returns how many cells line takes, leaving out escape sequences.
func width(line string) int {
	w := 0
	walk(line, func(_ string, cw int) bool {
		w += cw
		return true
	}, func(string) {})
	return w
}

// slice returns the n cells of line from cell from on, padded with spaces
// if the line ends before, and with the colors in effect there. A wide
// character cut by either edge becomes spaces. Colors are taken to be set
// whole and reset after, as package render paints.
func slice(line string, from, n int) string {
	var b strings.Builder
	end := from + n
	sgr := "" // the color in effect in line
	out := "" // the color in effect in what has been written
	put := func(s string) {
		if out != sgr {
			out = sgr
			if out == "" {
				b.WriteString("\x1b[0m")
			}
			b.WriteString(out)
		}
		b.WriteString(s)
	}
	x, filled := 0, 0
	walk(line, func(cluster string, w int) bool {
		switch {
		case x >= end:
			return false
		case x+w <= from:
		case x < from: // cut by the left edge
			put(strings.Repeat(" ", min(x+w, end)-from))
			filled += min(x+w, end) - from
		case x+w > end: // cut by the right edge
			put(strings.Repeat(" ", end-x))
			filled += end - x
		default:
			put(cluster)
			filled += w
		}
		x += w
		return true
	}, func(seq string) {
		switch {
		case !strings.HasSuffix(seq, "m"):
		case seq == "\x1b[0m" || seq == "\x1b[m":
			sgr = ""
		default:
			sgr = seq
		}
	})
	if out != "" {
		b.WriteString("\x1b[0m")
	}
	b.WriteString(strings.Repeat(" ", max(n-filled, 0)))
	return b.String()
}
