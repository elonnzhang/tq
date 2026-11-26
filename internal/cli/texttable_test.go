package cli

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/elonnzhang/tq/render"
)

func TestIsTextTable(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"LABEL            ENDPOINTS                                          SOURCE", true},
		{"74bdb031184ef2ba42f380743450d546        MIS     mis     default enabled", true},
		{"\x1b[1mLABEL\x1b[0m  ENDPOINTS  SOURCE\r\n", true},
		{"  name\tendpoint\tsource  ", true},
		{"1 x", false},
		{"1 x true", false},
		{"1 2 x", false},
		{"true invalid stuff", false},
		{"null invalid stuff", false},
		{`1 "two words" 3`, false},
		{"", false},
		{" \t\r\n", false},
		{"1 2 3", false},
		{"true false null", false},
		{"1 2.5 3e10", false},
		{`1 "two" {"three":3}`, false},
		{`{} [] "three"`, false},
		{`{"a": 1, broken`, false},
		{`[1, 2, broken`, false},
		{`"unterminated string here`, false},
		{`  { malformed JSON`, false},
	} {
		t.Run(tc.line, func(t *testing.T) {
			if got := isTextTable(tc.line); got != tc.want {
				t.Errorf("isTextTable(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestReadTextTable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  []any
	}{
		{
			name: "headed aligned cells",
			input: "\nLABEL            ENDPOINTS                                          SOURCE\n" +
				"local machine    unix:///tmp/rex.sock                               user config\n\n" +
				"remote           https://example.test                               discovery\n",
			want: []any{
				map[string]any{"LABEL": "local machine", "ENDPOINTS": "unix:///tmp/rex.sock", "SOURCE": "user config"},
				map[string]any{"LABEL": "remote", "ENDPOINTS": "https://example.test", "SOURCE": "discovery"},
			},
		},
		{
			name:  "headerless machine list",
			input: "74bdb031184ef2ba42f380743450d546        MIS     mis     default enabled\n",
			want:  []any{[]any{"74bdb031184ef2ba42f380743450d546", "MIS", "mis", "default", "enabled"}},
		},
		{
			name:  "lexical fields",
			input: "001 9007199254740993 true null 1e10\n002 42 false nil 0.0",
			want: []any{
				[]any{"001", "9007199254740993", "true", "null", "1e10"},
				[]any{"002", "42", "false", "nil", "0.0"},
			},
		},
		{
			name:  "fallback for all rows",
			input: "LABEL  ENDPOINTS  SOURCE\na  b  c\nd e f\n",
			want: []any{
				map[string]any{"LABEL": "a", "ENDPOINTS": "b", "SOURCE": "c"},
				map[string]any{"LABEL": "d", "ENDPOINTS": "e", "SOURCE": "f"},
			},
		},
		{
			name:  "single space header",
			input: "LABEL ENDPOINTS SOURCE\na b c\n",
			want:  []any{map[string]any{"LABEL": "a", "ENDPOINTS": "b", "SOURCE": "c"}},
		},
		{
			name:  "tabs and carriage returns",
			input: "\tLABEL\t\tENDPOINTS\tSOURCE\r\na b\t\tc\td e\r\n",
			want:  []any{map[string]any{"LABEL": "a b", "ENDPOINTS": "c", "SOURCE": "d e"}},
		},
		{
			name:  "ANSI styling",
			input: "\x1b[1mLABEL  ENDPOINTS  SOURCE\x1b[0m\n\x1b[32ma\x1b[0m  b  c\n",
			want:  []any{map[string]any{"LABEL": "a", "ENDPOINTS": "b", "SOURCE": "c"}},
		},
		{
			name:  "lowercase header remains data",
			input: "label endpoints source\na b c\n",
			want:  []any{[]any{"label", "endpoints", "source"}, []any{"a", "b", "c"}},
		},
		{
			name:  "non ASCII remains data",
			input: "名字 地址 来源\na b c\n",
			want:  []any{[]any{"名字", "地址", "来源"}, []any{"a", "b", "c"}},
		},
		{name: "empty", want: []any{}},
		{name: "blank", input: "\n \t\n", want: []any{}},
		{name: "headers only", input: "LABEL  ENDPOINTS  SOURCE\n\n", want: []any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readTextTable(strings.NewReader(tc.input), newKeyOrder())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("readTextTable = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestReadTextTableErrors(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"LABEL ENDPOINTS SOURCE\na b\n", "text table: line 2: expected 3 columns, got 2"},
		{"\na b c\n\nd e f g\n", "text table: line 4: expected 3 columns, got 4"},
		{"LABEL LABEL SOURCE\na b c\n", `text table: line 1: duplicate header "LABEL"`},
	} {
		_, err := readTextTable(strings.NewReader(tc.input), newKeyOrder())
		if err == nil || err.Error() != tc.want {
			t.Errorf("readTextTable(%q) error = %v, want %s", tc.input, err, tc.want)
		}
	}
	readErr := errors.New("read failed")
	_, err := readTextTable(textTableErrorReader{readErr}, newKeyOrder())
	if !errors.Is(err, readErr) {
		t.Errorf("read error = %v, want wrapped %v", err, readErr)
	}
}

type textTableErrorReader struct{ err error }

func (r textTableErrorReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadTextTableLongLine(t *testing.T) {
	value := strings.Repeat("x", 100_000)
	got, err := readTextTable(strings.NewReader(value+" b c"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []any{[]any{value, "b", "c"}}; !reflect.DeepEqual(got, want) {
		t.Error("long field was not preserved")
	}
}

func TestReadTextTableRender(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "header order",
			input: "LABEL  ENDPOINTS  SOURCE\na  b  c\n",
			want: "+-------+-----------+--------+\n" +
				"| LABEL | ENDPOINTS | SOURCE |\n" +
				"+-------+-----------+--------+\n" +
				"| a     | b         | c      |\n" +
				"+-------+-----------+--------+\n",
		},
		{
			name:  "headerless strings align left",
			input: "001  MIS  mis  default enabled\n2 X x main off\n",
			want: "+-----+-----+-----+---------+---------+\n" +
				"| 001 | MIS | mis | default | enabled |\n" +
				"| 2   | X   | x   | main    | off     |\n" +
				"+-----+-----+-----+---------+---------+\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			order := newKeyOrder()
			order.add("SOURCE") // Recorded source order wins for input objects.
			v, err := readTextTable(strings.NewReader(tc.input), order)
			if err != nil {
				t.Fatal(err)
			}
			var b strings.Builder
			if err := render.Render(&b, v, render.Options{Style: render.ASCII, Keys: order.keys}); err != nil {
				t.Fatal(err)
			}
			if got := b.String(); got != tc.want {
				t.Errorf("Render =\n%s\nwant:\n%s", got, tc.want)
			}
			if rows := v.([]any); len(rows) > 0 {
				if row, ok := rows[0].(map[string]any); ok {
					if got := order.keys(row); !slices.Equal(got, []string{"LABEL", "ENDPOINTS", "SOURCE"}) {
						t.Errorf("recorded keys = %v", got)
					}
				}
			}
		})
	}
}

func TestReadTextTableControlSafety(t *testing.T) {
	v, err := readTextTable(strings.NewReader("LABEL  ENDPOINTS  SOURCE\na\a  b\x1b[31m  c\x1b]0;injected\a\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := render.Render(&b, v, render.Options{}); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(b.String(), "\x1b\a") {
		t.Errorf("rendered input contains terminal controls: %q", b.String())
	}
}
