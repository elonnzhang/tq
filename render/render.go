// Package render draws JSON values as tables for people to read.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"
)

// Options controls how values are drawn.
type Options struct {
	// Width is the maximum line width in terminal cells, that is,
	// characters, with East Asian wide characters counting as two. Tables
	// wider than this get their columns narrowed and their text wrapped.
	// Zero means unlimited.
	Width int
	// Color enables ANSI colors.
	Color bool
	// Style selects the border characters. The zero value means Rounded.
	Style Style
	// Expand draws objects and arrays nested in table rows as tables
	// instead of "- key: value" lines.
	Expand bool
	// Keys returns the keys of an object in display order. The result is
	// not modified. Nil means sorted.
	Keys func(m map[string]any) []string
}

// SGR color codes.
const (
	colorKey   = "1;34" // headers and record keys
	colorFaint = "90"   // null, empty objects and arrays, and borders
)

// minColumn is how narrow fitting a table to the width limit may make a
// column; columns that are narrower to begin with keep their width. An
// array with too many columns to fit this way is drawn vertically instead,
// see vertical.
const minColumn = 10

// Render writes v to w. Objects are drawn as key/value tables, arrays of
// objects as tables with a column per key, arrays of arrays as tables with
// a row per array, other arrays as one-column tables, and scalars as plain
// text.
func Render(w io.Writer, v any, opts Options) error {
	return write(w, TablePage(v, opts))
}

// A Page is drawn output, for writing out or showing in a viewer.
type Page struct {
	Lines []string
	// Head is how many of the lines at the top are a table header, with
	// the borders around it, that a viewer can keep in view while
	// scrolling. Zero means there is none.
	Head int
}

// TablePage draws v the way Render writes it.
func TablePage(v any, opts Options) Page {
	if opts.Style == (Style{}) {
		opts.Style = Rounded
	}
	r := &renderer{opts: opts}
	c := r.block(v)
	if c.value == nil {
		return Page{Lines: r.plain(c)}
	}
	lines, head := r.table(c.value, opts.Width)
	return Page{Lines: lines, Head: head}
}

func write(w io.Writer, p Page) error {
	_, err := io.WriteString(w, strings.Join(p.Lines, "\n")+"\n")
	return err
}

type renderer struct {
	opts Options
}

// cell is one table cell: plain text, a nested value, or nothing at all
// for a key missing from some objects of an array. Text too wide for its
// column wraps onto more lines, so that it is always shown in full.
type cell struct {
	text  []string // plain text lines; unused when value is set
	color string   // SGR code applied to text
	num   bool     // holds a number
	right bool     // align right
	value any      // non-empty object or array drawn as a nested table
	lines []string // value drawn at its natural width, once measured
}

func (c *cell) missing() bool {
	return c.text == nil && c.value == nil
}

type table struct {
	header []*cell // column names; nil for tables without a header row
	rows   [][]*cell
	// group, when set, holds the record each row belongs to.
	group []int
}

// table draws a non-empty object or array, at most limit cells wide when
// the columns can be narrowed enough. A limit of zero means unlimited. It
// also returns how many of the lines are its header, as draw does.
func (r *renderer) table(v any, limit int) (lines []string, head int) {
	t := r.build(v)
	widths := r.widths(t)
	if limit > 0 && narrowest(widths) > limit {
		if alt := r.vertical(v); alt != nil {
			t, widths = alt, r.widths(alt)
		}
	}
	return r.draw(t, widths, limit)
}

// vertical lays out an array of objects or arrays with each object or
// array taking several lines, for when their fields don't fit side by side.
// It returns nil for other values.
func (r *renderer) vertical(v any) *table {
	arr, ok := v.([]any)
	switch {
	case !ok:
		return nil
	case r.columns(arr) != nil:
		return r.records(arr)
	case tupleWidth(arr) > 0:
		t := &table{}
		for _, e := range arr {
			t.rows = append(t.rows, []*cell{r.block(e)})
		}
		return t
	}
	return nil
}

func (r *renderer) build(v any) *table {
	t := &table{}
	switch v := v.(type) {
	case map[string]any:
		for _, k := range r.keys(v) {
			t.rows = append(t.rows, []*cell{r.key(k), r.block(v[k])})
		}
	case []any:
		if cols := r.columns(v); cols != nil {
			for _, k := range cols {
				t.header = append(t.header, r.key(k))
			}
			for _, e := range v {
				m := e.(map[string]any)
				row := make([]*cell, len(cols))
				for i, k := range cols {
					row[i] = &cell{}
					if x, ok := m[k]; ok {
						row[i] = r.inline(x)
					}
				}
				t.rows = append(t.rows, row)
			}
		} else if n := tupleWidth(v); n > 0 {
			for _, e := range v {
				a := e.([]any)
				row := make([]*cell, n)
				for i := range row {
					row[i] = &cell{}
					if i < len(a) {
						row[i] = r.inline(a[i])
					}
				}
				t.rows = append(t.rows, row)
			}
		} else {
			for _, e := range v {
				t.rows = append(t.rows, []*cell{r.block(e)})
			}
		}
		alignNumbers(t)
	default:
		t.rows = [][]*cell{{r.block(v)}}
	}
	return t
}

// columns returns the keys of the objects in arr in display order, or nil
// unless arr holds only objects and they have at least one key. A key
// missing from the objects before goes after the key it follows in the
// first object that has it, or before the key it precedes there.
func (r *renderer) columns(arr []any) []string {
	seen := map[string]bool{}
	var cols []string
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil
		}
		var lead []string // new keys before the first key in cols
		prev := ""        // the last key of m already in cols
		found := false
		for _, k := range r.keys(m) {
			if !seen[k] {
				seen[k] = true
				if !found {
					lead = append(lead, k)
					continue
				}
				cols = slices.Insert(cols, slices.Index(cols, prev)+1, k)
			} else if !found && lead != nil {
				cols = slices.Insert(cols, slices.Index(cols, k), lead...)
			}
			prev, found = k, true
		}
		if !found {
			cols = append(cols, lead...)
		}
	}
	return cols
}

// tupleWidth returns the length of the longest array in arr, or zero
// unless arr holds only arrays and one of them has an element.
func tupleWidth(arr []any) int {
	n := 0
	for _, e := range arr {
		a, ok := e.([]any)
		if !ok {
			return 0
		}
		n = max(n, len(a))
	}
	return n
}

// records lays out an array of objects as a key/value list per object, for
// objects with too many fields to fit side by side.
func (r *renderer) records(arr []any) *table {
	cols := r.columns(arr)
	t := &table{}
	for i, e := range arr {
		m := e.(map[string]any)
		if len(m) == 0 {
			t.rows = append(t.rows, []*cell{{}, empty(m)})
			t.group = append(t.group, i)
		}
		for _, k := range cols {
			if v, ok := m[k]; ok {
				t.rows = append(t.rows, []*cell{r.key(k), r.block(v)})
				t.group = append(t.group, i)
			}
		}
	}
	return t
}

// alignNumbers aligns the columns of t that hold only numbers to the right.
func alignNumbers(t *table) {
	for i := range t.rows[0] {
		right := false
		for _, row := range t.rows {
			if c := row[i]; !c.missing() {
				if !c.num {
					right = false
					break
				}
				right = true
			}
		}
		for _, row := range t.rows {
			row[i].right = right
		}
		if t.header != nil {
			t.header[i].right = right
		}
	}
}

// keys returns the keys of m in display order.
func (r *renderer) keys(m map[string]any) []string {
	if r.opts.Keys != nil {
		return r.opts.Keys(m)
	}
	return slices.Sorted(maps.Keys(m))
}

func (r *renderer) key(k string) *cell {
	return &cell{text: []string{oneLine(clean(k))}, color: colorKey}
}

// block makes the cell for a record value or list item, where nested
// values become tables.
func (r *renderer) block(v any) *cell {
	switch x := v.(type) {
	case map[string]any:
		if len(x) == 0 {
			return empty(x)
		}
		return &cell{value: x}
	case []any:
		if len(x) == 0 {
			return empty(x)
		}
		return &cell{value: x}
	}
	return r.scalar(v)
}

// inline makes the cell for a value in a table row, where nested values
// are written as "- key: value" lines unless Expand is set.
func (r *renderer) inline(v any) *cell {
	if r.opts.Expand {
		return r.block(v)
	}
	return r.inlineCell(v)
}

func (r *renderer) scalar(v any) *cell {
	switch v := v.(type) {
	case nil:
		return &cell{text: []string{"null"}, color: colorFaint}
	case bool:
		return &cell{text: []string{strconv.FormatBool(v)}}
	case string:
		return &cell{text: strings.Split(clean(v), "\n")}
	}
	b, err := gojq.Marshal(v)
	if err != nil {
		return &cell{text: []string{clean(fmt.Sprint(v))}}
	}
	return &cell{text: []string{string(b)}, num: isNumber(v)}
}

// empty makes the cell for an empty object or array.
func empty(v any) *cell {
	s := "[]"
	if _, ok := v.(map[string]any); ok {
		s = "{}"
	}
	return &cell{text: []string{s}, color: colorFaint}
}

func oneLine(s string) string {
	return strings.ReplaceAll(s, "\n", "↵")
}

func isNumber(v any) bool {
	switch v.(type) {
	case int, float64, *big.Int, json.Number:
		return true
	}
	return false
}

// natural returns the width c needs to be shown in full.
func (r *renderer) natural(c *cell) int {
	if c.value != nil {
		if c.lines == nil {
			c.lines, _ = r.table(c.value, 0)
		}
		return maxWidth(c.lines)
	}
	return maxWidth(c.text)
}

// fit returns the lines of c, each at most w cells wide.
func (r *renderer) fit(c *cell, w int) []string {
	if c.value != nil {
		lines := c.lines
		if maxWidth(lines) > w {
			lines, _ = r.table(c.value, w)
		}
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = truncate(l, w) // in case the nested table can't get that narrow
		}
		return out
	}
	var out []string
	for _, l := range c.text {
		for _, part := range wrap(l, w) {
			out = append(out, r.paint(c.color, part))
		}
	}
	return out
}
