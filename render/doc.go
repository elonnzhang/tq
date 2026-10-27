package render

import (
	"slices"
	"strconv"
	"strings"
)

// maxFlatColumns is how many columns a table in a document may have with
// the fields of nested objects spread over columns of their own, like
// "session.kind". Wider tables write nested objects as "- key: value"
// lines in one cell instead.
const maxFlatColumns = 5

// A docBlock is one block of a document: a heading, a paragraph, a bullet
// list or a table. Objects become sections with a heading per key, arrays
// of objects and arrays of arrays become tables, other arrays of scalars
// become lists, and scalars become paragraphs.
type docBlock struct {
	// heading holds the keys on the path to the section the heading
	// starts; its length is the heading level.
	heading []string
	depth   int   // how many sections the block is nested in
	para    *cell // paragraph lines
	code    bool  // whether para holds multi-line text to keep as is
	list    []*cell
	header  []string // table column names
	rows    [][]*cell
}

// doc lays out v as a document.
func (r *renderer) doc(v any) []docBlock {
	var blocks []docBlock
	r.section(&blocks, v, nil)
	return blocks
}

// section adds the blocks for v, the value at path, to blocks.
func (r *renderer) section(blocks *[]docBlock, v any, path []string) {
	add := func(b docBlock) {
		b.depth = len(path)
		*blocks = append(*blocks, b)
	}
	sub := func(k string, v any) {
		p := append(slices.Clip(path), k)
		add(docBlock{heading: p})
		r.section(blocks, v, p)
	}
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			add(docBlock{para: empty(x)})
		}
		for _, k := range r.keys(x) {
			sub(oneLine(clean(k)), x[k])
		}
	case []any:
		switch {
		case len(x) == 0:
			add(docBlock{para: empty(x)})
		case r.columns(x) != nil:
			header, rows := r.objectRows(x)
			add(docBlock{header: header, rows: rows})
		case tupleWidth(x) > 0:
			header, rows := r.tupleRows(x)
			add(docBlock{header: header, rows: rows})
		case !slices.ContainsFunc(x, isNested):
			var items []*cell
			for _, e := range x {
				items = append(items, r.inlineCell(e))
			}
			add(docBlock{list: items})
		default:
			for i, e := range x {
				sub("["+strconv.Itoa(i)+"]", e)
			}
		}
	case string:
		c := r.scalar(x)
		if x == "" {
			c = &cell{text: []string{`""`}, color: colorFaint}
		}
		add(docBlock{para: c, code: len(c.text) > 1})
	default:
		add(docBlock{para: r.scalar(x)})
	}
}

// objectRows lays out an array of objects as table rows with a column per
// key. The fields of nested objects get columns of their own unless that
// makes the table too wide.
func (r *renderer) objectRows(arr []any) ([]string, [][]*cell) {
	cols := r.flatColumns(arr, nil)
	if len(cols) > maxFlatColumns {
		cols = cols[:0]
		for _, k := range r.columns(arr) {
			cols = append(cols, []string{k})
		}
	}
	header := make([]string, len(cols))
	for i, p := range cols {
		header[i] = oneLine(clean(strings.Join(p, ".")))
	}
	rows := make([][]*cell, len(arr))
	for i, e := range arr {
		rows[i] = make([]*cell, len(cols))
		for j, p := range cols {
			rows[i][j] = &cell{}
			if v, ok := lookup(e, p); ok {
				rows[i][j] = r.inlineCell(v)
			}
		}
	}
	alignNumbers(&table{rows: rows})
	return header, rows
}

// flatColumns returns the paths of the columns for arr, an array of
// objects, with the fields of keys that hold only objects spread over
// columns of their own.
func (r *renderer) flatColumns(arr []any, prefix []string) [][]string {
	var cols [][]string
	for _, k := range r.columns(arr) {
		var vals []any
		for _, e := range arr {
			if v, ok := e.(map[string]any)[k]; ok {
				vals = append(vals, v)
			}
		}
		p := append(slices.Clip(prefix), k)
		if r.columns(vals) != nil {
			cols = append(cols, r.flatColumns(vals, p)...)
		} else {
			cols = append(cols, p)
		}
	}
	return cols
}

// lookup returns the value at path in v.
func lookup(v any, path []string) (any, bool) {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = m[k]; !ok {
			return nil, false
		}
	}
	return v, true
}

// tupleRows lays out an array of arrays as table rows with a column per
// index.
func (r *renderer) tupleRows(arr []any) ([]string, [][]*cell) {
	header := make([]string, tupleWidth(arr))
	for i := range header {
		header[i] = strconv.Itoa(i)
	}
	rows := make([][]*cell, len(arr))
	for i, e := range arr {
		a := e.([]any)
		rows[i] = make([]*cell, len(header))
		for j := range header {
			rows[i][j] = &cell{}
			if j < len(a) {
				rows[i][j] = r.inlineCell(a[j])
			}
		}
	}
	alignNumbers(&table{rows: rows})
	return header, rows
}

// inlineCell makes a text cell for v. Objects are written as "- key: value"
// lines, with the fields of nested objects indented under their key, and
// arrays of scalars as comma-separated lists.
func (r *renderer) inlineCell(v any) *cell {
	if !isNested(v) {
		return r.block(v)
	}
	return &cell{text: r.inlineLines(v)}
}

// inlineLines writes v as lines of text for a table cell.
func (r *renderer) inlineLines(v any) []string {
	var lines []string
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			return []string{"{}"}
		}
		for _, k := range r.keys(x) {
			lines = append(lines, r.entry(clean(k), x[k])...)
		}
	case []any:
		if len(x) == 0 {
			return []string{"[]"}
		}
		if !slices.ContainsFunc(x, isNested) {
			parts := make([]string, len(x))
			for i, e := range x {
				parts[i] = oneLine(strings.Join(r.inlineLines(e), "\n"))
			}
			return []string{strings.Join(parts, ", ")}
		}
		for i, e := range x {
			lines = append(lines, r.entry("["+strconv.Itoa(i)+"]", e)...)
		}
	case string:
		return strings.Split(clean(x), "\n")
	default:
		return r.scalar(x).text
	}
	return lines
}

// entry writes the "- key: value" lines for the field k with value v. The
// lines of a value spanning several go indented under the first.
func (r *renderer) entry(k string, v any) []string {
	val := r.inlineLines(v)
	head := "- " + k + ":"
	if !nestedList(v) {
		head += " " + val[0]
		val = val[1:]
	}
	lines := []string{head}
	for _, l := range val {
		lines = append(lines, "  "+l)
	}
	return lines
}

// nestedList reports whether inlineLines writes v as a list of entries.
func nestedList(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		return len(x) > 0
	case []any:
		return slices.ContainsFunc(x, isNested)
	}
	return false
}

// isNested reports whether v is an object or array with something in it.
func isNested(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		return len(x) > 0
	case []any:
		return len(x) > 0
	}
	return false
}

// headingText returns the text of the heading for path: the last key, or
// the whole path for headings nested too deep for their level to show.
func headingText(path []string, deep bool) string {
	if !deep {
		path = path[len(path)-1:]
	}
	var b strings.Builder
	for i, k := range path {
		if k == "" {
			k = `""`
		}
		if i > 0 && !strings.HasPrefix(k, "[") {
			b.WriteByte('.')
		}
		b.WriteString(k)
	}
	return b.String()
}
