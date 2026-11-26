package cli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestJQCompatibility compares results and status with jq, when installed.
// Scalar JSON text avoids depending on tq's table rendering or jq's indentation.
func TestJQCompatibility(t *testing.T) {
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is not installed")
	}
	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
	}{
		{"keys unsorted", `{"z":1,"a":2}`, []string{`keys_unsorted | tojson`}},
		{"array keys", `[9,8]`, []string{`keys_unsorted | tojson`}},
		{"entries", `{"z":1,"a":2}`, []string{`to_entries | tojson`}},
		{"nested entries", `{"b":{"z":1,"a":2}}`, []string{`.b | to_entries | tojson`}},
		{"entries array", `[9,8]`, []string{`to_entries | tojson`}},
		{"user entries", `{}`, []string{`def to_entries: "custom"; to_entries`}},
		{"repeated arg", "", []string{"-n", "--arg", "x", "first", "--arg", "x", "last", `[$x,$ARGS.named.x] | tojson`}},
		{"mixed repeated arg", "", []string{"-n", "--arg", "x", "first", "--argjson", "x", "2", `[$x,$ARGS.named.x] | tojson`}},
		{"numeric arg name", "", []string{"-n", "--arg", "1", "hi", `$ARGS.named["1"]`}},
		{"empty arg name", "", []string{"-n", "--arg", "", "hi", `$ARGS.named[""]`}},
		{"positional strings", "", []string{"-n", `[$ARGS.positional,$ARGS.named] | tojson`, "--args", "a", "b"}},
		{"positional JSON", "", []string{"-n", `$ARGS.positional | tojson`, "--jsonargs", "2", `{"a":1}`}},
		{"raw lines", "one\ntwo\n", []string{"-Rs", `split("\n") | tojson`}},
		{"raw final line", "one\ntwo", []string{"-Rs", `split("\n") | tojson`}},
		{"raw CRLF", "one\r\n", []string{"-R", `tojson`}},
		{"raw empty", "", []string{"-Rs", `length`}},
		{"null raw inputs", "one\ntwo\n", []string{"-Rn", `[inputs] | tojson`}},
		{"raw slurp input", "one\ntwo\n", []string{"-Rsn", `input | tojson`}},
		{"runtime recovery", `1 {"a":2}`, []string{`.a`}},
		{"runtime last", `{"a":2} 1`, []string{`.a`}},
		{"runtime empty recovery", `1 {}`, []string{`if type == "object" then empty else .a end`}},
		{"parse error", `{"a":`, []string{`.`}},
		{"parse partial result", `1 x`, []string{`.`}},
		{"slurp parse error", `1 x`, []string{"-s", `.`}},
		{"exit true", `true`, []string{"-e", `.`}},
		{"exit false", `false`, []string{"-e", `.`}},
		{"exit null", `null`, []string{"-e", `.`}},
		{"exit empty", `1`, []string{"-e", `empty`}},
		{"exit no input", ``, []string{"-e", `.`}},
		{"exit earlier result", `1 2`, []string{"-e", `if .==2 then empty else . end`}},
		{"exit runtime recovery", `1 {}`, []string{"-e", `.a`}},
		{"exit halt", ``, []string{"-ne", `halt`}},
		{"lookahead", `"ab"`, []string{`test("a(?=b)")`}},
		{"backref", `"aa"`, []string{`test("(.)\\1")`}},
		{"named capture", `"abc12"`, []string{`capture("(?<word>[a-z]+)(?<number>[0-9]+)") | [.word,.number] | tojson`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), jq, append([]string{"-r"}, tc.args...)...)
			cmd.Stdin = strings.NewReader(tc.stdin)
			var jqOut, jqErr strings.Builder
			cmd.Stdout, cmd.Stderr = &jqOut, &jqErr
			jqCode := 0
			if err := cmd.Run(); err != nil {
				if e, ok := errors.AsType[*exec.ExitError](err); ok {
					jqCode = e.ExitCode()
				} else {
					t.Fatal(err)
				}
			}
			out, errOut, code := tq(t, tc.stdin, tc.args...)
			if out != jqOut.String() || code != jqCode || (errOut == "") != (jqErr.Len() == 0) {
				t.Errorf("tq: (%d) %q %q\njq: (%d) %q %q", code, out, errOut, jqCode, jqOut.String(), jqErr.String())
			}
		})
	}
}

func TestCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
		out   string
		code  int
	}{
		{"keys", `{"z":1,"a":2}`, []string{`keys_unsorted | join(",")`}, "z,a\n", 0},
		{"entries", `{"z":1,"a":2}`, []string{`to_entries | map(.key) | join(",")`}, "z,a\n", 0},
		{"arg first", "", []string{"-n", "--arg", "x", "first", "--arg", "x", "last", `$x`}, "first\n", 0},
		{"raw input", "one\ntwo", []string{"-Rs", `length`}, "7\n", 0},
		{"raw lines", "one\ntwo\n", []string{"-Rn", `[inputs] | length`}, "2\n", 0},
		{"exit false", "false", []string{"-e", "."}, "false\n", 1},
		{"exit empty", "1", []string{"-e", "empty"}, "", 4},
		{"parse error", `{"a":`, []string{"."}, "", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, code := tq(t, tc.stdin, tc.args...)
			if out != tc.out || code != tc.code {
				t.Errorf("got (%d) %q, want (%d) %q", code, out, tc.code, tc.out)
			}
		})
	}
}

func TestInputFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := write("good.json", "3\n")
	bad := write("bad.json", "x\n")
	filter := write("filter.jq", ". + 1")
	for _, tc := range []struct {
		name string
		args []string
		out  string
		code int
	}{
		{"bad then good", []string{".", bad, good}, "", 5},
		{"missing then good", []string{".", filepath.Join(dir, "missing.json"), good}, "3\n", 2},
		{"slurp missing then good", []string{"-s", "tojson", filepath.Join(dir, "missing.json"), good}, "[3]\n", 2},
		{"raw slurp missing then good", []string{"-Rs", "length", filepath.Join(dir, "missing.json"), good}, "2\n", 2},
		{"filter file", []string{"-f", filter, good}, "4\n", 0},
		{"filter missing", []string{"-f", filepath.Join(dir, "missing.jq")}, "", 2},
		{"raw files", []string{"-Rs", "length", write("one.txt", "one"), write("two.txt", "two\n")}, "7\n", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, code := tq(t, "", tc.args...)
			if out != tc.out || code != tc.code {
				t.Errorf("got (%d) %q, want (%d) %q", code, out, tc.code, tc.out)
			}
		})
	}
}

type readFailure struct{}

func (readFailure) Read([]byte) (int, error) {
	return 0, errors.New("read past the JSON value")
}

func TestInputHaltDoesNotReadAhead(t *testing.T) {
	for _, value := range []string{`{}`, `[]`, `"text"`, "\n\t {}", "1 ", "true ", "false ", "null "} {
		t.Run(value, func(t *testing.T) {
			var out, errOut strings.Builder
			c := &cli{
				name: "tq", format: Table,
				stdin:  io.MultiReader(strings.NewReader(value), readFailure{}),
				stdout: &out, stderr: &errOut,
			}
			if code := c.run([]string{"-n", "input | halt"}); code != 0 || out.Len() != 0 || errOut.Len() != 0 {
				t.Errorf("got (%d) %q %q; want halt before reading more input", code, out.String(), errOut.String())
			}
		})
	}
}
