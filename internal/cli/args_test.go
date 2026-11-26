package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/itchyny/gojq"
)

func TestParseArgsCompatibilityFlags(t *testing.T) {
	for _, tc := range []struct {
		name       string
		args       []string
		filter     string
		files      []string
		exitStatus bool
		rawInput   bool
		fromFile   bool
		slurp      bool
	}{
		{name: "defaults"},
		{name: "short", args: []string{"-eR", "."}, filter: ".", exitStatus: true, rawInput: true},
		{name: "long after filter", args: []string{".", "--exit-status", "--raw-input", "input.json"}, filter: ".", files: []string{"input.json"}, exitStatus: true, rawInput: true},
		{name: "cluster with file", args: []string{"-sReffilter.jq", "input.json"}, filter: "filter.jq", files: []string{"input.json"}, exitStatus: true, rawInput: true, fromFile: true, slurp: true},
		{name: "short file separated", args: []string{"-f", "filter.jq", "input.json"}, filter: "filter.jq", files: []string{"input.json"}, fromFile: true},
		{name: "long file", args: []string{"--from-file", "filter.jq"}, filter: "filter.jq", fromFile: true},
		{name: "long file equals", args: []string{"--from-file=filter.jq"}, filter: "filter.jq", fromFile: true},
		{name: "file after filter", args: []string{"first.jq", "-f", "second.jq"}, filter: "first.jq", files: []string{"second.jq"}, fromFile: true},
		{name: "file value starting dash", args: []string{"-f", "-e"}, filter: "-e", fromFile: true},
		{name: "empty filter", args: []string{"", "input.json"}, files: []string{"input.json"}},
		{name: "options ended", args: []string{"--", "-R", "-e"}, filter: "-R", files: []string{"-e"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parseArgs(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if o.filter != tc.filter || !slices.Equal(o.files, tc.files) ||
				o.exitStatus != tc.exitStatus || o.rawInput != tc.rawInput ||
				o.fromFile != tc.fromFile || o.slurp != tc.slurp {
				t.Errorf("parseArgs(%q) = %+v; want filter=%q files=%q exitStatus=%v rawInput=%v fromFile=%v slurp=%v", tc.args, o, tc.filter, tc.files, tc.exitStatus, tc.rawInput, tc.fromFile, tc.slurp)
			}
			if o.positional == nil || len(o.positional) != 0 {
				t.Errorf("positional = %#v; want an empty array", o.positional)
			}
		})
	}
}

func TestParseArgsPositionalModes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		filter string
		files  []string
		want   []any
	}{
		{name: "filter first", args: []string{"--args", "$ARGS", "a", "b"}, filter: "$ARGS", want: []any{"a", "b"}},
		{name: "earlier files stay files with args", args: []string{".", "before", "--args", "after"}, filter: ".", files: []string{"before"}, want: []any{"after"}},
		{name: "earlier files stay files with jsonargs", args: []string{".", "invalid JSON filename", "--jsonargs", "true"}, filter: ".", files: []string{"invalid JSON filename"}, want: []any{true}},
		{name: "earlier files with no values", args: []string{".", "before", "--jsonargs"}, filter: ".", files: []string{"before"}, want: []any{}},
		{name: "JSON types", args: []string{"--jsonargs", ".", "null", "false", `"text"`, `[1]`, `{"x":2}`}, filter: ".", want: []any{nil, false, "text", []any{json.Number("1")}, map[string]any{"x": json.Number("2")}}},
		{name: "mixed modes", args: []string{"--args", ".", "1", "--jsonargs", "2", "--args", "3"}, filter: ".", want: []any{"1", json.Number("2"), "3"}},
		{name: "repeat mode", args: []string{"--args", ".", "a", "--args", "b"}, filter: ".", want: []any{"a", "b"}},
		{name: "options still parsed", args: []string{"--args", ".", "a", "-eR", "--arg", "x", "value", "b"}, filter: ".", want: []any{"a", "b"}},
		{name: "double dash strings", args: []string{"--args", ".", "--", "--jsonargs", "-e", "--"}, filter: ".", want: []any{"--jsonargs", "-e", "--"}},
		{name: "double dash JSON", args: []string{"--jsonargs", ".", "--", "-2", `"--args"`}, filter: ".", want: []any{json.Number("-2"), "--args"}},
		{name: "double dash before filter", args: []string{"--args", "--", "-filter", "a"}, filter: "-filter", want: []any{"a"}},
		{name: "file filter", args: []string{"--args", "-f", "filter.jq", "a"}, filter: "filter.jq", want: []any{"a"}},
		{name: "JSON file filter", args: []string{"--jsonargs", "--from-file=filter.jq", "1"}, filter: "filter.jq", want: []any{json.Number("1")}},
		{name: "no values", args: []string{"--jsonargs", "."}, filter: ".", want: []any{}},
		{name: "no filter", args: []string{"--args"}, want: []any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parseArgs(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if o.filter != tc.filter || !slices.Equal(o.files, tc.files) || !reflect.DeepEqual(o.positional, tc.want) {
				t.Errorf("parseArgs(%q): filter=%q files=%q positional=%#v; want filter=%q files=%q positional=%#v", tc.args, o.filter, o.files, o.positional, tc.filter, tc.files, tc.want)
			}
			if tc.name == "options still parsed" && (!o.exitStatus || !o.rawInput || o.named["x"] != "value") {
				t.Errorf("options were not parsed: %+v", o)
			}
		})
	}
}

func TestParseArgsCompatibilityErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"-f"}, "-f needs 1 argument(s)"},
		{[]string{"-eRf"}, "-f needs 1 argument(s)"},
		{[]string{"--from-file"}, "--from-file needs 1 argument(s)"},
		{[]string{"--raw-input=true"}, "unknown option: --raw-input=true"},
		{[]string{"--exit-status=true"}, "unknown option: --exit-status=true"},
		{[]string{"--args=x"}, "unknown option: --args=x"},
		{[]string{"--jsonargs=x"}, "unknown option: --jsonargs=x"},
		{[]string{"-eZ"}, "unknown option: -Z"},
		{[]string{"--args", ".", "--unknown"}, "unknown option: --unknown"},
		{[]string{"--jsonargs", ".", "invalid"}, "--jsonargs: invalid JSON:"},
		{[]string{"--jsonargs", ".", "--", "--args"}, "--jsonargs: invalid JSON:"},
		{[]string{"--jsonargs", ".", "1 2"}, "--jsonargs: invalid JSON:"},
		{[]string{"--arg", "x"}, "--arg needs 2 argument(s)"},
		{[]string{"--argjson", "x", "invalid"}, "--argjson x: invalid JSON:"},
		{[]string{"--arg", "x", "first", "--argjson", "x"}, "--argjson needs 2 argument(s)"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			_, err := parseArgs(tc.args)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("parseArgs(%q) error = %v; want %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestNamedArgsFirstBinding(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want any
	}{
		{[]string{"--arg", "x", "first", "--arg", "x", "second"}, "first"},
		{[]string{"--arg", "x", "first", "--argjson", "x", "invalid"}, "first"},
		{[]string{"--argjson", "x", "null", "--argjson", "x", "invalid"}, nil},
		{[]string{"--argjson", "x", "2", "--arg", "x", "second"}, json.Number("2")},
		{[]string{"--argjson", "x", "false", "--argjson", "x", "true"}, false},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			o, err := parseArgs(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(o.named, map[string]any{"x": tc.want}) ||
				!slices.Equal(o.names, []string{"$x"}) || !reflect.DeepEqual(o.values, []any{tc.want}) {
				t.Errorf("named=%#v names=%q values=%#v; want first binding %#v", o.named, o.names, o.values, tc.want)
			}
		})
	}
}

func TestNamedArgsVariableNames(t *testing.T) {
	for _, name := range []string{"x", "_", "x2", "ARGS", "", "a-b", "a.b", "a[0]", "a::b", "a # comment", "a | .", "1x", "$x", "名字", "é"} {
		t.Run(name, func(t *testing.T) {
			o, err := parseArgs([]string{"--arg", name, "first", "--argjson", name, "invalid"})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(o.named, map[string]any{name: "first"}) {
				t.Errorf("named = %#v; want arbitrary name %q with first binding", o.named, name)
			}
			valid := slices.Contains([]string{"x", "_", "x2"}, name)
			if valid {
				if !slices.Equal(o.names, []string{"$" + name}) || !reflect.DeepEqual(o.values, []any{"first"}) {
					t.Errorf("names=%q values=%#v; want variable for %q", o.names, o.values, name)
				}
			} else if len(o.names) != 0 || len(o.values) != 0 {
				t.Errorf("names=%q values=%#v; %q belongs only in named", o.names, o.values, name)
			}
			q, err := gojq.Parse("$ARGS.named")
			if err != nil {
				t.Fatal(err)
			}
			code, err := gojq.Compile(q, gojq.WithVariables(append(o.names, "$ARGS")))
			if err != nil {
				t.Fatalf("caller cannot compile variables: %v", err)
			}
			values := append(o.values, map[string]any{"named": o.named, "positional": o.positional})
			got, ok := code.Run(nil, values...).Next()
			if !ok || !reflect.DeepEqual(got, o.named) {
				t.Errorf("builtin ARGS.named = %#v, %v; want %#v", got, ok, o.named)
			}
		})
	}
}

func TestParseArgsUsageCompatibility(t *testing.T) {
	help := usage("tq", Table, Print)
	for _, option := range []string{"-e, --exit-status", "-R, --raw-input", "-f, --from-file FILE", "--args", "--jsonargs", "--input FORMAT", "-R takes precedence"} {
		if !strings.Contains(help, option) {
			t.Errorf("help does not mention %q", option)
		}
	}
	for line := range strings.SplitSeq(help, "\n") {
		if len(line) >= 80 {
			t.Errorf("help line is %d columns: %q", len(line), line)
		}
	}
}

func TestParseArgsInputFormat(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
		raw  bool
	}{
		{name: "default", want: "auto"},
		{name: "auto", args: []string{"--input", "auto"}, want: "auto"},
		{name: "json", args: []string{"--input", "json"}, want: "json"},
		{name: "table equals", args: []string{"--input=table"}, want: "table"},
		{name: "after filter", args: []string{".", "--input", "table"}, want: "table"},
		{name: "last wins", args: []string{"--input=table", "--input=json"}, want: "json"},
		{name: "raw first", args: []string{"-R", "--input=table"}, want: "table", raw: true},
		{name: "raw last", args: []string{"--input=table", "-R"}, want: "table", raw: true},
		{name: "positional mode", args: []string{"--args", ".", "--input=table", "value"}, want: "table"},
		{name: "options ended", args: []string{".", "--", "--input=table"}, want: "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parseArgs(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if o.input != tc.want || o.rawInput != tc.raw {
				t.Errorf("parseArgs(%q): input=%q rawInput=%v; want input=%q rawInput=%v", tc.args, o.input, o.rawInput, tc.want, tc.raw)
			}
		})
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--input"}, "--input needs 1 argument(s)"},
		{[]string{"--input=unknown"}, `invalid input format: "unknown"`},
		{[]string{"--input", "JSON"}, `invalid input format: "JSON"`},
		{[]string{"--input="}, `invalid input format: ""`},
		{[]string{"-R", "--input=unknown"}, `invalid input format: "unknown"`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			_, err := parseArgs(tc.args)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Errorf("parseArgs(%q) error = %v; want %q", tc.args, err, tc.want)
			}
		})
	}
}

func TestParseArgsJQOracle(t *testing.T) {
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is not installed")
	}
	prior := filepath.Join(t.TempDir(), "prior.json")
	if err := os.WriteFile(prior, []byte(`{"source":"prior"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const filter = "{input: ., args: $ARGS}"
	for _, tc := range []struct {
		name  string
		args  []string
		files []string
	}{
		{name: "prior file before args", args: []string{filter, prior, "--args", "after"}, files: []string{prior}},
		{name: "prior file before jsonargs", args: []string{filter, prior, "--jsonargs", "true"}, files: []string{prior}},
		{name: "prior file with empty jsonargs", args: []string{filter, prior, "--jsonargs"}, files: []string{prior}},
		{name: "strings JSON strings", args: []string{"-n", "--args", filter, "1", "--jsonargs", "2", "--args", "3", "-e", "--", "-x"}},
		{name: "JSON strings JSON", args: []string{"-n", "--jsonargs", filter, "1", "--args", "2", "--jsonargs", "true", "--", "-3"}},
		{name: "reserved ARGS", args: []string{"-n", "--arg", "ARGS", "first", "--argjson", "ARGS", "invalid", "--args", filter, "value"}},
		{name: "reserved JSON ARGS", args: []string{"-n", "--argjson", "ARGS", "null", "--arg", "ARGS", "second", filter}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, err := parseArgs(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if o.filter != filter || !slices.Equal(o.files, tc.files) {
				t.Fatalf("filter=%q files=%q; want filter=%q files=%q", o.filter, o.files, filter, tc.files)
			}
			out, err := exec.CommandContext(t.Context(), jq, tc.args...).CombinedOutput()
			if err != nil {
				t.Fatalf("jq %q: %v\n%s", tc.args, err, out)
			}
			want, err := parseJSON(string(out))
			if err != nil {
				t.Fatalf("parse jq output: %v", err)
			}
			var input any
			if len(o.files) != 0 {
				data, err := os.ReadFile(o.files[0])
				if err != nil {
					t.Fatal(err)
				}
				input, err = parseJSON(string(data))
				if err != nil {
					t.Fatal(err)
				}
			}
			q, err := gojq.Parse(o.filter)
			if err != nil {
				t.Fatal(err)
			}
			code, err := gojq.Compile(q, gojq.WithVariables(append(o.names, "$ARGS")))
			if err != nil {
				t.Fatal(err)
			}
			values := append(o.values, map[string]any{"named": o.named, "positional": o.positional})
			got, ok := code.Run(input, values...).Next()
			if !ok {
				t.Fatal("gojq returned no result")
			}
			data, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("encode gojq result: %v", err)
			}
			got, err = parseJSON(string(data))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("parser bindings produce %#v; jq produces %#v", got, want)
			}
		})
	}
}
