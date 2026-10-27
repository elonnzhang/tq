package render

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// decode parses s the way tq feeds values to Render.
func decode(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", s, err)
	}
	return v
}

func check(t *testing.T, input string, opts Options, want string) {
	t.Helper()
	var b strings.Builder
	if err := Render(&b, decode(t, input), opts); err != nil {
		t.Fatal(err)
	}
	want = strings.TrimPrefix(want, "\n")
	if got := b.String(); got != want {
		t.Errorf("Render(%s) =\n%s\nwant:\n%s", input, got, want)
	}
}

func TestScalars(t *testing.T) {
	for input, want := range map[string]string{
		`"text"`:               "text\n",
		`"two\nlines"`:         "two\nlines\n",
		`12345678901234567890`: "12345678901234567890\n",
		`1.5`:                  "1.5\n",
		`true`:                 "true\n",
		`null`:                 "null\n",
		`{}`:                   "{}\n",
		`[]`:                   "[]\n",
	} {
		check(t, input, Options{}, want)
	}
}

func TestObject(t *testing.T) {
	check(t, `{"name":"tq","tags":[],"meta":{},"n":1,"ok":true,"none":null}`, Options{}, `
╭──────┬──────╮
│ meta │ {}   │
│ n    │ 1    │
│ name │ tq   │
│ none │ null │
│ ok   │ true │
│ tags │ []   │
╰──────┴──────╯
`)
}

func TestNestedObject(t *testing.T) {
	check(t, `{"a":{"b":1,"c":[1,2]},"d":"x"}`, Options{}, `
╭───┬───────────────╮
│ a │ ╭───┬───────╮ │
│   │ │ b │ 1     │ │
│   │ ├───┼───────┤ │
│   │ │ c │ ╭───╮ │ │
│   │ │   │ │ 1 │ │ │
│   │ │   │ │ 2 │ │ │
│   │ │   │ ╰───╯ │ │
│   │ ╰───┴───────╯ │
├───┼───────────────┤
│ d │ x             │
╰───┴───────────────╯
`)
}

func TestArrayOfObjects(t *testing.T) {
	// Missing keys leave cells empty; numeric columns align right; nested
	// values become "- key: value" lines and comma-separated lists.
	check(t, `[{"name":"a","size":5,"tags":["x","y"]},{"name":"bb","size":120,"meta":{"k":1,"v":{"w":"z"}}},{"name":"c"}]`, Options{}, `
╭──────────┬──────┬──────┬──────╮
│ meta     │ name │ size │ tags │
├──────────┼──────┼──────┼──────┤
│          │ a    │    5 │ x, y │
├──────────┼──────┼──────┼──────┤
│ - k: 1   │ bb   │  120 │      │
│ - v:     │      │      │      │
│   - w: z │      │      │      │
├──────────┼──────┼──────┼──────┤
│          │ c    │      │      │
╰──────────┴──────┴──────┴──────╯
`)
}

func TestArrayOfArrays(t *testing.T) {
	check(t, `[[1,"a",true],[22,"bb"],[333]]`, Options{}, `
╭─────┬────┬──────╮
│   1 │ a  │ true │
│  22 │ bb │      │
│ 333 │    │      │
╰─────┴────┴──────╯
`)
}

func TestList(t *testing.T) {
	check(t, `[1,"two",{"a":1},[]]`, Options{}, `
╭───────────╮
│ 1         │
│ two       │
├───────────┤
│ ╭───┬───╮ │
│ │ a │ 1 │ │
│ ╰───┴───╯ │
├───────────┤
│ []        │
╰───────────╯
`)
}

func TestOrder(t *testing.T) {
	// Keys gives the order within each object; the columns of an array
	// of objects merge those orders, placing keys new to a later object
	// next to the keys around them there.
	rank := []string{"z", "a", "b", "c", "d"}
	keys := func(m map[string]any) []string {
		var ks []string
		for _, k := range rank {
			if _, ok := m[k]; ok {
				ks = append(ks, k)
			}
		}
		return ks
	}
	check(t, `{"a":1,"z":2}`, Options{Keys: keys}, `
╭───┬───╮
│ z │ 2 │
│ a │ 1 │
╰───┴───╯
`)
	check(t, `[{"a":1,"c":3},{"b":2,"c":3},{"c":3,"d":4},{"z":0}]`, Options{Keys: keys}, `
╭───┬───┬───┬───┬───╮
│ a │ b │ c │ d │ z │
├───┼───┼───┼───┼───┤
│ 1 │   │ 3 │   │   │
│   │ 2 │ 3 │   │   │
│   │   │ 3 │ 4 │   │
│   │   │   │   │ 0 │
╰───┴───┴───┴───┴───╯
`)
}

func TestWidthLimit(t *testing.T) {
	// The widest column gives way first, and text that no longer fits
	// wraps onto more lines rather than being cut.
	check(t, `[{"id":1,"path":"/a/very/long/path/to/somewhere"},{"id":2,"path":"/b"}]`, Options{Width: 30}, `
╭────┬───────────────────────╮
│ id │ path                  │
├────┼───────────────────────┤
│  1 │ /a/very/long/path/to/ │
│    │ somewhere             │
├────┼───────────────────────┤
│  2 │ /b                    │
╰────┴───────────────────────╯
`)
	check(t, `{"id":1,"path":"/a/very/long/path/to/somewhere"}`, Options{Width: 30}, `
╭──────┬─────────────────────╮
│ id   │ 1                   │
├──────┼─────────────────────┤
│ path │ /a/very/long/path/t │
│      │ o/somewhere         │
╰──────┴─────────────────────╯
`)
}

func TestRecordsFallback(t *testing.T) {
	// Columns that can't all fit at minColumn turn into a list of records.
	check(t, `[{"alpha":"aaaaaaaaaaaa","bravo":"bbbbbbbbbbbb","charlie":"cccccccccccc"},{"alpha":"x"}]`, Options{Width: 36}, `
╭─────────┬──────────────╮
│ alpha   │ aaaaaaaaaaaa │
│ bravo   │ bbbbbbbbbbbb │
│ charlie │ cccccccccccc │
├─────────┼──────────────┤
│ alpha   │ x            │
╰─────────┴──────────────╯
`)
}

func TestWideCharacters(t *testing.T) {
	check(t, `[{"名前":"東京タワー","n":1},{"名前":"ab","n":22}]`, Options{Style: ASCII}, `
+----+------------+
|  n | 名前       |
+----+------------+
|  1 | 東京タワー |
| 22 | ab         |
+----+------------+
`)
}

func TestExpand(t *testing.T) {
	check(t, `[{"a":{"x":1},"b":"long text that wraps"}]`, Options{Expand: true, Width: 30}, `
╭───────────┬────────────────╮
│ a         │ b              │
├───────────┼────────────────┤
│ ╭───┬───╮ │ long text that │
│ │ x │ 1 │ │ wraps          │
│ ╰───┴───╯ │                │
╰───────────┴────────────────╯
`)
}

func TestEscapes(t *testing.T) {
	// Control characters in keys and values must not reach the terminal,
	// where they could break the layout or run escape sequences.
	rlo := string(rune(0x202e)) // makes text after it read right to left
	v := map[string]any{
		"k\x1b]0;x": "red\x1b[31m\a",
		"bidi":      "a" + rlo + "b",
		"tab":       "a\tb",
	}
	var b strings.Builder
	if err := Render(&b, v, Options{}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	if strings.ContainsAny(got, "\x1b\a\t"+rlo) {
		t.Errorf("Render(%q) = %q, has control characters", v, got)
	}
	for _, want := range []string{"k␛]0;x", "red␛[31m␇", `a\u` + "202eb", "a    b"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render(%q) = %q, want it to contain %q", v, got, want)
		}
	}

	// Line breaks in text show as line breaks in table cells too.
	check(t, `[{"s":"x\ny"}]`, Options{}, `
╭───╮
│ s │
├───┤
│ x │
│ y │
╰───╯
`)
}

func TestColor(t *testing.T) {
	var b strings.Builder
	if err := Render(&b, decode(t, `{"a":null}`), Options{Color: true}); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	for _, want := range []string{"\x1b[1;34ma\x1b[0m", "\x1b[90mnull\x1b[0m"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render with color = %q, want it to contain %q", got, want)
		}
	}
}

func TestShrink(t *testing.T) {
	for _, tc := range []struct {
		rows  [][]int // how wide the text of each cell is, by row
		avail int
		want  []int
	}{
		{[][]int{{3, 4}}, 10, []int{3, 4}},
		{[][]int{{3, 40}}, 20, []int{3, 17}},
		{[][]int{{2, 30, 40}}, 4, []int{2, 10, 10}}, // can't go below minColumn
		// The room goes where it saves the most lines: all of the second
		// column's cells are long, only one of the first column's.
		{[][]int{{40, 30}, {5, 30}, {5, 30}}, 50, []int{20, 30}},
	} {
		tbl := &table{}
		for _, ws := range tc.rows {
			row := make([]*cell, len(ws))
			for j, w := range ws {
				row[j] = &cell{text: []string{strings.Repeat("x", w)}}
			}
			tbl.rows = append(tbl.rows, row)
		}
		r := &renderer{}
		if got := r.shrink(tbl, r.widths(tbl), tc.avail); !slices.Equal(got, tc.want) {
			t.Errorf("shrink(%v, %d) = %v, want %v", tc.rows, tc.avail, got, tc.want)
		}
	}
}

func TestWrap(t *testing.T) {
	for _, tc := range []struct {
		s    string
		w    int
		want []string
	}{
		{"short", 10, []string{"short"}},
		{"long text that wraps", 14, []string{"long text that", "wraps"}},
		{"a verylongword b", 5, []string{"a", "veryl", "ongwo", "rd b"}},
		{"東京タワー", 4, []string{"東京", "タワ", "ー"}},
	} {
		if got := wrap(tc.s, tc.w); !slices.Equal(got, tc.want) {
			t.Errorf("wrap(%q, %d) = %q, want %q", tc.s, tc.w, got, tc.want)
		}
	}
}
