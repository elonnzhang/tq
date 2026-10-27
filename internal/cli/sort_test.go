package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestParseSortKey(t *testing.T) {
	for _, tc := range []struct {
		spec string
		path string // as pathString writes it
		desc bool
		err  string
	}{
		{spec: ".id", path: ".id"},
		{spec: "id", path: ".id"},
		{spec: "-.data.id", path: ".data.id", desc: true},
		{spec: "-data.id", path: ".data.id", desc: true},
		{spec: ".data[].id", path: ".data[].id"},
		{spec: ".data.[].id", path: ".data[].id"},
		{spec: `."owned by"`, path: `."owned by"`},
		{spec: `.["a.b"].c`, path: `."a.b".c`},
		{spec: ".items[0].size", path: ".items[0].size"},
		{spec: "[1]", path: "[1]"},
		{spec: ".", path: "."},
		{spec: "-", err: "empty path"},
		{spec: "--.data.id", err: "one - is enough to sort in reverse"},
		{spec: ".a[", err: "missing ]"},
		{spec: ".a[x]", err: "bad index [x]"},
		{spec: ".a[-1]", err: "bad index [-1]"},
		{spec: ".a[][]", err: "more than one []"},
		{spec: ".a..b", err: "empty key"},
		{spec: `."a`, err: `missing closing "`},
	} {
		k, err := parseSortKey(tc.spec)
		switch {
		case tc.err != "":
			if err == nil || err.Error() != tc.err {
				t.Errorf("parseSortKey(%q) = %v, want error %q", tc.spec, err, tc.err)
			}
		case err != nil:
			t.Errorf("parseSortKey(%q): %v", tc.spec, err)
		case pathString(k.path) != tc.path || k.desc != tc.desc:
			t.Errorf("parseSortKey(%q) = %s, desc %v; want %s, desc %v",
				tc.spec, pathString(k.path), k.desc, tc.path, tc.desc)
		}
	}
}

func TestCompareValues(t *testing.T) {
	// Each value sorts before the ones after it.
	for _, list := range [][]any{
		{"-1.5", ".5", 2, "9", "10", 1e3, "12345678901234567890122", json.Number("12345678901234567890123")},
		{"a2", "A10", "a10", "b"},
		{"Claude Opus 4.8", "Claude Opus 5", "claude opus 5.5"},
		{"v1.9", "v1.10", "v2"},
		{"File", "file", "file1"},
		{false, true, map[string]any{}},
	} {
		for i := range list {
			for j := range list {
				want := 0
				switch {
				case i < j:
					want = -1
				case i > j:
					want = 1
				}
				if got := compareValues(list[i], list[j]); got != want {
					t.Errorf("compareValues(%#v, %#v) = %d, want %d", list[i], list[j], got, want)
				}
			}
		}
	}
}

func TestSort(t *testing.T) {
	const data = `{"data":[
		{"id":"m10","n":"9","g":"b"},
		{"id":"M2","n":10,"g":"a"},
		{"id":"m1","n":null,"g":"b"},
		{"id":"m3","g":"a"},
		{"id":"m20","n":-1,"g":"b","sub":[{"id":"y"},{"id":"x"}]}
	],"object":"list"}`
	for _, tc := range []struct {
		sorts []string
		want  string // the ids in .data, and in its sub arrays after a /
	}{
		{[]string{".data.id"}, "m1 M2 m3 m10 m20 / y x"},
		{[]string{"data.id"}, "m1 M2 m3 m10 m20 / y x"},
		{[]string{".data[].id"}, "m1 M2 m3 m10 m20 / y x"},
		{[]string{"-.data.id"}, "m20 / y x m10 m3 M2 m1"},
		// Every array whose elements have the path.
		{[]string{".id"}, "m1 M2 m3 m10 m20 / x y"},
		// Numbers and numeric strings by value, then null and missing.
		{[]string{".n"}, "m20 / y x m10 M2 m1 m3"},
		{[]string{"-n"}, "M2 m10 m20 / y x m1 m3"},
		// Ties are broken by the keys after.
		{[]string{".g", "-.n"}, "M2 m3 m10 m20 / y x m1"},
		{[]string{".g", ".id"}, "M2 m3 m1 m10 m20 / x y"},
	} {
		v, err := parseJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range slices.Backward(tc.sorts) {
			k, err := parseSortKey(s)
			if err == nil {
				err = sortBy(v, k)
			}
			if err != nil {
				t.Fatalf("--sort %s: %v", s, err)
			}
		}
		var got []string
		for _, e := range v.(map[string]any)["data"].([]any) {
			got = append(got, e.(map[string]any)["id"].(string))
			if sub, ok := e.(map[string]any)["sub"].([]any); ok {
				got = append(got, "/")
				for _, e := range sub {
					got = append(got, e.(map[string]any)["id"].(string))
				}
			}
		}
		if s := strings.Join(got, " "); s != tc.want {
			t.Errorf("--sort %q: %s, want %s", tc.sorts, s, tc.want)
		}
	}
}

func TestSortErrors(t *testing.T) {
	const data = `{"a":[{"b":1}],"c":"x"}`
	for _, tc := range []struct{ spec, err string }{
		{".nope", "the output has no .nope, and no array in the output has elements with .nope"},
		{".a.c", "no element of .a has .c"},
		{".a[].c", "no element of .a has .c"},
		{".c", ".c is a string, not an array, and no array in the output has elements with .c"},
		{".c[]", ".c is a string, not an array"},
		{".", ". is an object, not an array"},
	} {
		v, _ := parseJSON(data)
		k, err := parseSortKey(tc.spec)
		if err == nil {
			err = sortBy(v, k)
		}
		if err == nil || err.Error() != tc.err {
			t.Errorf("--sort %s: %v, want %s", tc.spec, err, tc.err)
		}
	}
}

func TestSortIndex(t *testing.T) {
	for _, tc := range []struct{ spec, in, want string }{
		// Rows of arrays, by a column.
		{"[0]", `[[3,"c"],[1,"a"],[2,"b"]]`, `[[1,"a"],[2,"b"],[3,"c"]]`},
		{"-.rows[1]", `{"rows":[[3,"c"],[1,"a"]]}`, `{"rows":[[3,"c"],[1,"a"]]}`},
		{".rows[1]", `{"rows":[[3,"c"],[1,"a"]]}`, `{"rows":[[1,"a"],[3,"c"]]}`},
		// An element, to sort an array in it.
		{".data[1].items.x", `{"data":[{"items":[{"x":2},{"x":1}]},{"items":[{"x":9},{"x":8}]}]}`,
			`{"data":[{"items":[{"x":2},{"x":1}]},{"items":[{"x":8},{"x":9}]}]}`},
	} {
		v, _ := parseJSON(tc.in)
		k, err := parseSortKey(tc.spec)
		if err == nil {
			err = sortBy(v, k)
		}
		got, _ := json.Marshal(v)
		if err != nil || string(got) != tc.want {
			t.Errorf("--sort %s on %s = %s, %v; want %s", tc.spec, tc.in, got, err, tc.want)
		}
	}
}
