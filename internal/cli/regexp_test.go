package cli

import (
	"encoding/json"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/itchyny/gojq"
)

func runRegexpQuery(t *testing.T, input any, filter string) ([]any, error) {
	t.Helper()
	q, err := gojq.Parse(filter)
	if err != nil {
		t.Fatal(err)
	}
	code, err := gojq.Compile(q, regexpOption())
	if err != nil {
		t.Fatal(err)
	}
	results := make([]any, 0)
	iter := code.Run(input)
	for {
		v, ok := iter.Next()
		if !ok {
			return results, nil
		}
		if err, ok := v.(error); ok {
			return results, err
		}
		results = append(results, v)
	}
}

func checkRegexpJSON(t *testing.T, got any, want string) {
	t.Helper()
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected any
	if err := json.Unmarshal(data, &actual); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("got %s\nwant %s", data, want)
	}
}

func TestRegexp(t *testing.T) {
	tests := []struct {
		name, input, filter, want string
	}{
		{"lookahead", "foobar", `match("foo(?=bar)")`, `[{"offset":0,"length":3,"string":"foo","captures":[]}]`},
		{"lookbehind", "foobar", `match("(?<=foo)bar"; null)`, `[{"offset":3,"length":3,"string":"bar","captures":[]}]`},
		{"negative assertions", "food", `test("foo(?!bar)")`, `[true]`},
		{"negative lookbehind", "foobar", `test("(?<!foo)bar"; "")`, `[false]`},
		{"numeric backrefs", "ababa", `match("(?<first>a)(b)\\1\\2\\1") | [.string, .captures[].name]`, `[["ababa","first",null]]`},
		{"interleaved captures", "abcd", `match("(a)(?<b>b)(c)(?<d>d)") | [.captures[].name]`, `[[null,"b",null,"d"]]`},
		{"named backref", "éé", `test("(?<letter>é)\\k<letter>")`, `[true]`},
		{"unicode captures", "é🙂b", `match("(?<first>é)(🙂)(z)?()")`, `[{"offset":0,"length":2,"string":"é🙂","captures":[{"name":"first","offset":0,"length":1,"string":"é"},{"name":null,"offset":1,"length":1,"string":"🙂"},{"name":null,"offset":-1,"length":0,"string":null},{"name":null,"offset":2,"length":0,"string":""}]}]`},
		{"unicode global", "🙂é🙂", `match("🙂"; "g") | [.offset,.length]`, `[[0,1],[2,1]]`},
		{"repeated capture", "aaa", `match("(a)+") | .captures[0].offset`, `[2]`},
		{"empty global", "aba", `match("a*"; "g") | [.offset,.length]`, `[[0,1],[1,0],[2,1],[3,0]]`},
		{"lazy empty global", "aba", `match(".*?"; "g") | [.offset,.length]`, `[[0,0],[1,0],[2,0],[3,0]]`},
		{"ignore empty", "aba", `match("a*"; "gn") | [.offset,.length]`, `[[0,1],[2,1]]`},
		{"lazy nonempty", "aba", `match(".*?"; "gn") | [.offset,.length]`, `[[0,1],[1,1],[2,1]]`},
		{"alternate nonempty", "aba", `match("|a"; "gn") | [.offset,.length]`, `[[0,1],[2,1]]`},
		{"empty test", "abc", `test(""; "n")`, `[false]`},
		{"nonempty test", "abc", `test("|b"; "n")`, `[true]`},
		{"nonempty capture", "abc", `capture("(?<a>.*?)"; "n")`, `[{"a":"a"}]`},
		{"empty input", "", `match(""; "g") | [.offset,.length]`, `[[0,0]]`},
		{"no matches", "abc", `match("z")`, `[]`},
		{"ignorecase", "ABC", `test("abc"; "ii")`, `[true]`},
		{"jq flags", "a\nb\nc", `[test("^b"),test("^b";"s"),test("a.b"),test("a.b";"m"),test("a.b";"p")]`, `[[false,false,false,true,true]]`},
		{"inline flags", "a\nb\nc", `[test("(?m)^b"),test("(?s)^b"),test("(?m)a.b"),test("(?s)a.b")]`, `[[true,false,false,true]]`},
		{"extended", "a b", `test(" a \\x20 b # comment"; "x")`, `[true]`},
		{"POSIX class hash", "#ab", `match("(?x)[[:alpha:]#]ab") | [.offset,.length,.string]`, `[[0,3,"#ab"]]`},
		{"POSIX class duplicate nonempty", "#ab", `match("(?x)[[:alpha:]#](?<x>a)(?<x>b)"; "n") | [.string,.captures[].name]`, `[["#ab","x","x"]]`},
		{"extended nonempty", "ab", `match(" a*? # comment"; "xgn") | .string`, `["a"]`},
		{"inline extended nonempty", "ab", `match("(?x)a*? # comment"; "gn") | [.offset,.string]`, `[[0,"a"]]`},
		{"scoped extended nonempty", "ab", `match("(?x:a*?) # not a comment"; "gn") | .string`, `[]`},
		{"scoped comment nonempty", "ab", `match("(?x:a*? # comment\n)"; "gn") | .string`, `["a"]`},
		{"disabled extended nonempty", "a#", `match("(?x)(?-x)a*?#"; "gn") | .string`, `["a#"]`},
		{"open quote nonempty", "ab", `match("a\\Q"; "n") | [.offset,.string]`, `[[0,"a"]]`},
		{"open quote literals", "a#(?<x>\\b", `match("a\\Q#(?<x>\\b"; "xn") | .string`, `["a#(?<x>\\b"]`},
		{"closed quote literals", "aa", `match("\\Qa\\E*?"; "gn") | .string`, `["a","a"]`},
		{"duplicate scan", "ab", `scan("(?<x>a)(?<x>b)")`, `[["a","b"]]`},
		{"duplicate captures", "ab", `match("(?<x>a)(?<x>b)")`, `[{"offset":0,"length":2,"string":"ab","captures":[{"name":"x","offset":0,"length":1,"string":"a"},{"name":"x","offset":1,"length":1,"string":"b"}]}]`},
		{"duplicate numeric backrefs", "abab", `scan("(?<x>a)(?<x>b)\\1\\2")`, `[["a","b"]]`},
		{"duplicate backref latest", "abb", `test("(?<x>a)(?<x>b)\\k<x>")`, `[true]`},
		{"duplicate backref earlier", "aba", `test("(?<x>a)(?<x>b)\\k<x>")`, `[true]`},
		{"duplicate unmatched backref", "aa", `scan("(?<x>a)(?<x>b)?\\k<x>")`, `[["a",null]]`},
		{"duplicate named condition", "ac", `scan("(?<x>a)?(?<x>b)?(?(<x>)c|d)")`, `[["a",null]]`},
		{"duplicate empty condition", "d", `scan("(?<x>a)?(?<x>b)?(?(<x>)c|d)")`, `[[null,null]]`},
		{"duplicate backref order", "aaaaa", `match("(?<x>a)(?<x>aa)\\k<x>") | .length`, `[5]`},
		{"duplicate backref length fallback", "aaaa", `match("(?<x>a)(?<x>aa)\\k<x>") | .length`, `[4]`},
		{"duplicate alternation", "bb", `scan("(?:(?<x>a)|(?<x>b))\\k<x>")`, `[[null,"b"]]`},
		{"duplicate nonempty", "ab", `match("(?x)(?<x>a*?)(?<x>b*?) # comment"; "gn") | [.string,.captures[].name]`, `[["a","x","x"],["b","x","x"]]`},
		{"duplicate capture object", "ab", `capture("(?<x>a)(?<x>b)")`, `[{"x":"b"}]`},
		{"duplicate sub", "ab", `sub("(?<x>a)(?<x>b)"; .x)`, `["b"]`},
		{"duplicate lookbehind", "ab", `scan("(?<=(?<x>a))(?<x>b)")`, `[["a","b"]]`},
		{"quoted declaration", "(?<x>a)b", `scan("\\Q(?<x>a)\\E(?<x>b)")`, `[["b"]]`},
		{"comment declaration", "b", `scan("(?x)# (?<x>a)\n(?<x>b)")`, `[["b"]]`},
		{"group comment declaration", "b", `scan("(?# (?<x>a)(?<x>b)")`, `[["b"]]`},
		{"internal name collision", "ab", `scan("(?<tqCapture0>a)(?<tqCapture0>b)")`, `[["a","b"]]`},
		{"capture", "abc", `capture("(?<a>a)(?<b>z)?")`, `[{"a":"a","b":null}]`},
		{"capture flags", "ABC", `capture("(?<a>a)"; "i")`, `[{"a":"A"}]`},
		{"scan", "abac", `scan("a(?=.)")`, `["a","a"]`},
		{"scan captures", "abac", `scan("(?<a>a)(.)"; "")`, `[["a","b"],["a","c"]]`},
		{"sub", "abac", `sub("(?<a>a)(?=b)"; "X" + .a)`, `["Xabac"]`},
		{"sub flags", "ABAC", `sub("(?<a>a)(?=b)"; .a + "!"; "i")`, `["A!BAC"]`},
		{"gsub", "abac", `gsub("(?<a>a)(?=.)"; .a + "!")`, `["a!ba!c"]`},
		{"gsub flags", "ABAC", `gsub("a(?=.)"; "x"; "i")`, `["xBxC"]`},
		{"sub no match", "abc", `sub("z"; "x")`, `["abc"]`},
		{"gsub empty", "ab", `gsub(""; "x")`, `["xaxbx"]`},
		{"splits", "a,b;c", `splits("[,;](?=.)")`, `["a","b","c"]`},
		{"split flags", "aBcdBef", `split("b(?=.)"; "i")`, `[["a","cd","ef"]]`},
		{"direct hook", "abc", `_match("a(?=b)"; null; true)`, `[true]`},
		{"user override", "abc", `def match($re): "custom"; match("a")`, `["custom"]`},
		{"unicode empty advancement", "é🙂b", `match(""; "g") | .offset`, `[0,1,2,3]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runRegexpQuery(t, tt.input, tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			checkRegexpJSON(t, got, tt.want)
		})
	}
}

func TestRegexpErrors(t *testing.T) {
	for _, tt := range []struct {
		name, filter, want string
		input              any
	}{
		{"bad pattern", `test("(")`, "invalid regular expression", "a"},
		{"empty duplicate names", `test("(?<>a)(?<>b)")`, "invalid regular expression", "ab"},
		{"bad flag", `test("a"; "q")`, "unsupported regular expression flag", "a"},
		{"longest", `test("a|ab"; "l")`, "longest matching is not supported", "ab"},
		{"input type", `test("a")`, "requires string input", 1},
		{"pattern type", `test(1)`, "requires string input", "a"},
		{"flags type", `test("a"; 1)`, "requires string input", "a"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := runRegexpQuery(t, tt.input, tt.filter)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestRegexpTimeout(t *testing.T) {
	start := time.Now()
	_, err := runRegexpQuery(t, strings.Repeat("a", 1000)+"!", `test("^(a+)+$")`)
	if err == nil {
		t.Fatal("expected bounded backtracking error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("backtracking took %s", elapsed)
	}
	if strings.Contains(err.Error(), strings.Repeat("a", 100)) {
		t.Fatal("error includes input")
	}
}

// The oracle supplements fixed fixtures without making jq a test dependency.
func TestRegexpJQOracle(t *testing.T) {
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is not installed")
	}
	for _, tt := range []struct{ input, filter string }{
		{"#ab", `[match("(?x)[[:alpha:]#]ab")]`},
		{"#ab", `[match("(?x)[[:alpha:]#](?<x>a)(?<x>b)"; "n")]`},
		{"é🙂b", `[match("(?<first>é)(🙂)(z)?()")]`},
		{"é🙂b", `[match(".*?"; "gn")]`},
		{"é🙂b", `[match("|🙂"; "gn")]`},
		{"é🙂b", `[capture("(?<a>é)(?<b>x)?")]`},
		{"é🙂b", `[scan("(?<a>é)(🙂)")]`},
		{"é🙂b", `[sub("(?<a>é)(?=🙂)"; .a + "!")]`},
		{"é🙂b", `[gsub("(?<=é)🙂"; "x")]`},
		{"é🙂b", `[gsub(".*?"; "x"; "n")]`},
		{"ab", `[match("(?x)a*? # comment"; "gn")]`},
		{"ab", `[match("(?x:a*? # comment\n)"; "gn")]`},
		{"a#", `[match("(?x)(?-x)a*?#"; "gn")]`},
		{"ab", `[match("a\\Q"; "n")]`},
		{"a#(?<x>\\b", `[match("a\\Q#(?<x>\\b"; "xn")]`},
		{"aa", `[match("\\Qa\\E*?"; "gn")]`},
		{"ab", `[match("(?<x>a)(?<x>b)")]`},
		{"ab", `[scan("(?<x>a)(?<x>b)")]`},
		{"abab", `[scan("(?<x>a)(?<x>b)\\1\\2")]`},
		{"aba", `[match("(?<x>a)(?<x>b)\\k<x>")]`},
		{"abb", `[match("(?<x>a)(?<x>b)\\k<x>")]`},
		{"aaaaa", `[match("(?<x>a)(?<x>aa)\\k<x>")]`},
		{"aaaa", `[match("(?<x>a)(?<x>aa)\\k<x>")]`},
		{"aa", `[match("(?<x>a)(?<x>b)?\\k<x>")]`},
		{"ac", `[match("(?<x>a)?(?<x>b)?(?(<x>)c|d)")]`},
		{"d", `[match("(?<x>a)?(?<x>b)?(?(<x>)c|d)")]`},
		{"bb", `[match("(?:(?<x>a)|(?<x>b))\\k<x>")]`},
		{"ab", `[match("(?x)(?<x>a*?)(?<x>b*?) # comment"; "gn")]`},
		{"ab", `[capture("(?<x>a)(?<x>b)")]`},
		{"ab", `[sub("(?<x>a)(?<x>b)"; .x)]`},
		{"ab", `[scan("(?<=(?<x>a))(?<x>b)")]`},
		{"b", `[scan("(?x)# (?<x>a)\n(?<x>b)")]`},
		{"b", `[scan("(?# (?<x>a)(?<x>b)")]`},
	} {
		t.Run(tt.input+"/"+tt.filter, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), jq, "-c", tt.filter)
			data, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stdin = strings.NewReader(string(data))
			want, err := cmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			got, err := runRegexpQuery(t, tt.input, tt.filter)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("got %d results", len(got))
			}
			checkRegexpJSON(t, got[0], string(want))
		})
	}
}
