package render

import (
	"slices"
	"strings"
)

// widths returns the width each column of t needs to show its cells in
// full.
func (r *renderer) widths(t *table) []int {
	n := len(t.header)
	for _, row := range t.rows {
		n = max(n, len(row))
	}
	ws := make([]int, n)
	measure := func(row []*cell) {
		for i, c := range row {
			ws[i] = max(ws[i], r.natural(c))
		}
	}
	if t.header != nil {
		measure(t.header)
	}
	for _, row := range t.rows {
		measure(row)
	}
	return ws
}

// border returns how many cells the borders and padding of a table with n
// columns take up.
func border(n int) int {
	return 3*n + 1
}

// narrowest returns how wide a table with columns of the given natural
// widths gets when they are narrowed as far as shrink goes.
func narrowest(widths []int) int {
	sum := border(len(widths))
	for _, w := range widths {
		sum += min(w, minColumn)
	}
	return sum
}

// draw lays out t, narrowing its columns from their natural widths to fit
// in limit cells if needed. It also returns how many of the lines are the
// header row and the borders around it, or zero if t has no header.
func (r *renderer) draw(t *table, widths []int, limit int) (lines []string, head int) {
	if limit > 0 {
		widths = r.shrink(t, widths, limit-border(len(widths)))
	}

	// Nested tables redrawn for a narrower column may come out narrower
	// still, so the final widths are taken from the fitted lines.
	final := make([]int, len(widths))
	fitRow := func(row []*cell) [][]string {
		out := make([][]string, len(row))
		for i, c := range row {
			out[i] = r.fit(c, widths[i])
			final[i] = max(final[i], maxWidth(out[i]))
		}
		return out
	}
	var header [][]string
	if t.header != nil {
		header = fitRow(t.header)
	}
	rows := make([][][]string, len(t.rows))
	for i, row := range t.rows {
		rows[i] = fitRow(row)
	}
	// Rules separate records, or else rows spanning several lines from the
	// rows around them.
	ruled := func(i int) bool {
		if t.group != nil {
			return t.group[i] != t.group[i-1]
		}
		return height(rows[i-1]) > 1 || height(rows[i]) > 1
	}

	st := r.opts.Style
	out := []string{r.rule(st.TL, st.TM, st.TR, final)}
	if header != nil {
		out = append(out, r.line(t.header, header, final)...)
		out = append(out, r.rule(st.ML, st.MM, st.MR, final))
		head = len(out)
	}
	for i := range rows {
		if i > 0 && ruled(i) {
			out = append(out, r.rule(st.ML, st.MM, st.MR, final))
		}
		out = append(out, r.line(t.rows[i], rows[i], final)...)
	}
	return append(out, r.rule(st.BL, st.BM, st.BR, final)), head
}

// shrink narrows the columns of t from their natural widths so that they
// add up to at most avail. Each column starts at minColumn, or at its
// natural width if that is less, and the cells left over go to the columns
// where they save the most lines, so that columns get room by how much
// they hold. No column gets narrower than minColumn unless it already was,
// so the result may still exceed avail.
func (r *renderer) shrink(t *table, natural []int, avail int) []int {
	total := 0
	for _, n := range natural {
		total += n
	}
	if total <= avail {
		return natural
	}
	ws := make([]int, len(natural))
	used := 0
	for j, n := range natural {
		ws[j] = min(n, minColumn)
		used += ws[j]
	}
	g := r.lineGrid(t, ws)
	for used < avail {
		// Try widening each column by 1, 2, 4… cells, since a column may
		// need several more before any of its lines fit in one fewer.
		best := widening{col: -1}
		for j := range ws {
			room := min(natural[j]-ws[j], avail-used)
			for d := 1; room > 0; d *= 2 {
				d = min(d, room)
				if s := g.try(j, ws[j]+d); s.better(best) {
					best = s
				}
				if d == room {
					break
				}
			}
		}
		if best.col < 0 {
			break
		}
		// Take only as many cells as saving those lines needs.
		j, lo, hi := best.col, ws[best.col], ws[best.col]+best.cells
		for lo+1 < hi {
			if mid := (lo + hi) / 2; g.try(j, mid).lines == best.lines {
				hi = mid
			} else {
				lo = mid
			}
		}
		used += hi - ws[j]
		ws[j] = hi
		g.set(j, hi)
	}
	// Whatever is left saves no lines in the rows looked at, but may in
	// others.
	for used < avail && !slices.Equal(ws, natural) {
		for j := range ws {
			if used < avail && ws[j] < natural[j] {
				ws[j]++
				used++
			}
		}
	}
	return ws
}

// maxSample is how many rows of a table shrink looks at.
const maxSample = 1000

// lineGrid estimates how many lines the cells of a table take at given
// column widths, taking each line of a cell to wrap into lines of the
// column width.
type lineGrid struct {
	widths []int     // the current column widths
	cells  [][][]int // the widths of the lines of each cell, by row and column
	lines  [][]int   // how many lines each cell takes at the current widths
	// The tallest cell of each row, its column, and the next tallest.
	h1, c1, h2 []int
}

// lineGrid makes the grid for t at widths ws, with its header and at most
// maxSample rows spread across it.
func (r *renderer) lineGrid(t *table, ws []int) *lineGrid {
	var rows [][]*cell
	if t.header != nil {
		rows = append(rows, t.header)
	}
	step := max(1, len(t.rows)/maxSample)
	for i := 0; i < len(t.rows); i += step {
		rows = append(rows, t.rows[i])
	}
	g := &lineGrid{
		widths: slices.Clone(ws),
		cells:  make([][][]int, len(rows)),
		lines:  make([][]int, len(rows)),
		h1:     make([]int, len(rows)),
		c1:     make([]int, len(rows)),
		h2:     make([]int, len(rows)),
	}
	for i, row := range rows {
		g.cells[i] = make([][]int, len(ws))
		g.lines[i] = make([]int, len(ws))
		for j := range ws {
			if j < len(row) {
				g.cells[i][j] = r.lineWidths(row[j])
			}
			g.lines[i][j] = countLines(g.cells[i][j], ws[j])
		}
		g.rank(i)
	}
	return g
}

// lineWidths returns the widths of the lines c takes at its natural width.
// Nested tables get redrawn to fit their column rather than wrapped, so
// their lines count as one each at any width.
func (r *renderer) lineWidths(c *cell) []int {
	if c.value != nil {
		r.natural(c)
		return make([]int, len(c.lines))
	}
	ws := make([]int, len(c.text))
	for i, l := range c.text {
		ws[i] = width(l)
	}
	return ws
}

// countLines estimates how many lines text whose lines are as wide as
// widths takes when wrapped to w cells.
func countLines(widths []int, w int) int {
	n := 0
	for _, lw := range widths {
		n += max(1, (lw+w-1)/w)
	}
	return max(n, 1)
}

// rank finds the tallest cells of row i.
func (g *lineGrid) rank(i int) {
	g.h1[i], g.c1[i], g.h2[i] = 0, -1, 0
	for j, n := range g.lines[i] {
		if n > g.h1[i] {
			g.h2[i], g.h1[i], g.c1[i] = g.h1[i], n, j
		} else if n > g.h2[i] {
			g.h2[i] = n
		}
	}
}

// widening is the effect of widening a column.
type widening struct {
	col, cells int
	rows       int // how many fewer lines the rows take
	lines      int // how many fewer lines the cells of the column take
}

// better reports whether w saves more lines per cell of width than o. Lines
// saved in the rows count, and so do lines saved within cells, since text
// wrapped in a cell reads worse even where the row doesn't get shorter.
func (w widening) better(o widening) bool {
	if w.lines == 0 {
		return false
	}
	if o.col < 0 {
		return true
	}
	return (w.rows+w.lines)*o.cells > (o.rows+o.lines)*w.cells
}

// try returns the effect of widening column j to w cells.
func (g *lineGrid) try(j, w int) widening {
	s := widening{col: j, cells: w - g.widths[j]}
	for i := range g.cells {
		n := countLines(g.cells[i][j], w)
		others := g.h1[i]
		if g.c1[i] == j {
			others = g.h2[i]
		}
		s.rows += g.h1[i] - max(n, others)
		s.lines += g.lines[i][j] - n
	}
	return s
}

// set makes column j w cells wide.
func (g *lineGrid) set(j, w int) {
	g.widths[j] = w
	for i := range g.cells {
		g.lines[i][j] = countLines(g.cells[i][j], w)
		g.rank(i)
	}
}

func height(cells [][]string) int {
	h := 1
	for _, c := range cells {
		h = max(h, len(c))
	}
	return h
}

// line draws one table row, which spans as many lines as its tallest cell.
func (r *renderer) line(cells []*cell, fitted [][]string, widths []int) []string {
	v := r.paint(colorFaint, r.opts.Style.V)
	lines := make([]string, height(fitted))
	for y := range lines {
		var b strings.Builder
		b.WriteString(v)
		for i, w := range widths {
			s := ""
			if i < len(fitted) && y < len(fitted[i]) {
				s = fitted[i][y]
			}
			pad := strings.Repeat(" ", max(0, w-width(s)))
			b.WriteByte(' ')
			if i < len(cells) && cells[i].right {
				b.WriteString(pad)
				b.WriteString(s)
			} else {
				b.WriteString(s)
				b.WriteString(pad)
			}
			b.WriteByte(' ')
			b.WriteString(v)
		}
		lines[y] = b.String()
	}
	return lines
}

// rule draws a horizontal border.
func (r *renderer) rule(left, mid, right string, widths []int) string {
	var b strings.Builder
	b.WriteString(left)
	for i, w := range widths {
		if i > 0 {
			b.WriteString(mid)
		}
		b.WriteString(strings.Repeat(r.opts.Style.H, w+2))
	}
	b.WriteString(right)
	return r.paint(colorFaint, b.String())
}

func (r *renderer) paint(color, s string) string {
	if !r.opts.Color || color == "" || s == "" {
		return s
	}
	return "\x1b[" + color + "m" + s + "\x1b[0m"
}
