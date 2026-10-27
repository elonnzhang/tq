package render

import (
	"io"
	"strings"
)

// indent is how far each level of sections is indented in Doc.
const indent = 2

// Doc writes v to w as a document laid out for reading in a terminal. It
// has the structure Markdown writes, with each section indented under its
// heading and tables drawn with borders; tables with too many columns to
// fit list their rows as records instead.
func Doc(w io.Writer, v any, opts Options) error {
	return write(w, DocPage(v, opts))
}

// DocPage draws v the way Doc writes it. A document that is just a table
// has that table's header for its Head.
func DocPage(v any, opts Options) Page {
	if opts.Style == (Style{}) {
		opts.Style = Rounded
	}
	r := &renderer{opts: opts}
	var page Page
	blocks := r.doc(v)
	for i, blk := range blocks {
		pad := strings.Repeat(" ", indent*blk.depth)
		avail := 0 // unlimited
		if opts.Width > 0 {
			avail = max(opts.Width-len(pad), minColumn)
		}
		var lines []string
		switch {
		case blk.heading != nil:
			level := len(blk.heading)
			if i > 0 && level == 1 {
				page.Lines = append(page.Lines, "")
			}
			lines = []string{r.paint(colorFaint, strings.Repeat("#", level)) + " " +
				r.paint(colorKey, headingText(blk.heading, false))}
		case blk.para != nil:
			lines = r.text(blk.para, avail)
		case blk.list != nil:
			for _, c := range blk.list {
				for j, l := range r.text(c, max(avail-2, 0)) {
					if j == 0 {
						lines = append(lines, r.paint(colorFaint, "•")+" "+l)
					} else {
						lines = append(lines, "  "+l)
					}
				}
			}
		default:
			var head int
			lines, head = r.docTable(blk.header, blk.rows, avail)
			if len(blocks) == 1 {
				page.Head = head
			}
		}
		for _, l := range lines {
			page.Lines = append(page.Lines, pad+l)
		}
	}
	return page
}

// text returns the lines of the text cell c wrapped to w cells, or as they
// are if w is zero.
func (r *renderer) text(c *cell, w int) []string {
	if w <= 0 {
		return r.plain(c)
	}
	return r.fit(c, w)
}

// plain returns the lines of the text cell c, painted.
func (r *renderer) plain(c *cell) []string {
	lines := make([]string, len(c.text))
	for i, l := range c.text {
		lines[i] = r.paint(c.color, l)
	}
	return lines
}

// docTable draws a table of a document at most limit cells wide, or as a
// list of records if its columns can't be narrowed enough to fit. It also
// returns how many of the lines are its header, as draw does.
func (r *renderer) docTable(header []string, rows [][]*cell, limit int) ([]string, int) {
	t := &table{}
	for j, h := range header {
		t.header = append(t.header, &cell{text: []string{h}, color: colorKey, right: rows[0][j].right})
	}
	t.rows = rows
	widths := r.widths(t)
	if limit > 0 && narrowest(widths) > limit {
		t = &table{}
		for i, row := range rows {
			for j, c := range row {
				if c.missing() {
					continue
				}
				val := *c
				val.right = false
				t.rows = append(t.rows, []*cell{{text: []string{header[j]}, color: colorKey}, &val})
				t.group = append(t.group, i)
			}
		}
		widths = r.widths(t)
	}
	return r.draw(t, widths, limit)
}
