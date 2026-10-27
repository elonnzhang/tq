package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/itchyny/gojq"
)

// tq runs the command line args on stdin and returns what it printed and
// its exit code.
func tq(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return run(t, &cli{name: "tq", format: Table}, stdin, args...)
}

// run runs c with args on stdin, with stdout and stderr captured.
func run(t *testing.T, c *cli, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut strings.Builder
	c.stdin = strings.NewReader(stdin)
	c.stdout, c.stderr = &out, &errOut
	c.environ = func() []string { return []string{"TQ_TEST=yes"} }
	code = c.run(args)
	return out.String(), errOut.String(), code
}

func TestHerdrAgentList(t *testing.T) {
	data, err := os.ReadFile("testdata/herdr-agent-list.json")
	if err != nil {
		t.Fatal(err)
	}
	// The filter's keys come first, in the order written.
	out, errOut, code := tq(t, string(data), "-w", "80",
		`.result.agents[] | {pane_id, agent, status: .agent_status, cwd}`)
	want := `
╭─────────┬────────┬─────────┬───────────────────╮
│ pane_id │ agent  │ status  │ cwd               │
├─────────┼────────┼─────────┼───────────────────┤
│ w5:pJ   │ pi     │ idle    │ /home/dev/src/api │
│ w5:p12  │ claude │ working │ /home/dev/src/tq  │
│ w13:p2  │ claude │ done    │ /home/dev/src/bot │
╰─────────┴────────┴─────────┴───────────────────╯
`[1:]
	if out != want || errOut != "" || code != 0 {
		t.Errorf("got (%d)\n%s%s\nwant:\n%s", code, out, errOut, want)
	}

	// Without a filter, keys keep the order of the input. The agents
	// don't fit side by side in 80 columns, so each becomes a record.
	out, _, code = tq(t, string(data), "-w", "80")
	for _, want := range []string{"│ id     │ cli:agent:list", "│ agent                    │ pi", "│ terminal_title_stripped  │ tq：表格化 jq"} {
		if !strings.Contains(out, want) {
			t.Errorf("tq = %d\n%s\nwant it to contain %q", code, out, want)
		}
	}
	if i, j := strings.Index(out, "agent_status"), strings.Index(out, "completion_seq"); i < 0 || j < i {
		t.Errorf("keys are out of input order:\n%s", out)
	}
	if lines := strings.Split(out, "\n"); maxLineWidth(lines) > 80 {
		t.Errorf("output is wider than 80 columns:\n%s", out)
	}
}

func maxLineWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, len([]rune(l)))
	}
	return w
}

func TestCLI(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stdin  string
		args   []string
		stdout string
		stderr string
		code   int
	}{
		{
			name:  "scalar",
			stdin: `{"a":{"b":"text"}}`, args: []string{".a.b"},
			stdout: "text\n",
		},
		{
			name:  "results are collected",
			stdin: `[1,2]`, args: []string{".[]"},
			stdout: "╭───╮\n│ 1 │\n│ 2 │\n╰───╯\n",
		},
		{
			name:  "each input is run",
			stdin: `{"n":1} {"n":2}`, args: []string{".n"},
			stdout: "╭───╮\n│ 1 │\n│ 2 │\n╰───╯\n",
		},
		{
			name:  "slurp",
			stdin: `1 2 3`, args: []string{"-s", "add"},
			stdout: "6\n",
		},
		{
			name:   "null input with args",
			args:   []string{"-n", "--arg", "x", "hi", "--argjson", "y", "[1]", `"\($x) \($y[0]) \($ENV.TQ_TEST)"`},
			stdout: "hi 1 yes\n",
		},
		{
			name:  "inputs",
			stdin: `1 2`, args: []string{"-n", "[inputs] | length"},
			stdout: "2\n",
		},
		{
			name:  "short flags combine",
			stdin: `1 2`, args: []string{"-sMw20", "length"},
			stdout: "2\n",
		},
		{
			name:  "no results",
			stdin: `{}`, args: []string{"empty"},
		},
		{
			name:   "invalid filter",
			args:   []string{".["},
			stderr: "tq: invalid filter: unexpected EOF\n",
			code:   3,
		},
		{
			name:  "runtime error",
			stdin: `1 {"a":2}`, args: []string{".a"},
			stdout: "2\n",
			stderr: "tq: error: expected an object but got: number (1)\n",
			code:   5,
		},
		{
			name:  "invalid JSON",
			stdin: `{"a":1} x`, args: []string{".a"},
			stdout: "1\n",
			stderr: "tq: <stdin>: invalid JSON at byte 9: invalid character 'x' looking for beginning of value\n",
			code:   2,
		},
		{
			name:   "truncated JSON",
			stdin:  `{"a":`,
			stderr: "tq: <stdin>: invalid JSON: unexpected end of input\n",
			code:   2,
		},
		{
			name:   "missing file",
			args:   []string{".", "testdata/missing.json"},
			stderr: "tq: open testdata/missing.json: no such file or directory\n",
			code:   2,
		},
		{
			name:   "halt_error",
			args:   []string{"-n", `"bye\n" | halt_error(4)`},
			stderr: "bye\n",
			code:   4,
		},
		{
			name:   "unknown option",
			args:   []string{"--bogus"},
			stderr: "tq: unknown option: --bogus\nRun 'tq --help' for usage.\n",
			code:   2,
		},
		{
			name:  "sort",
			stdin: `[9,null,"10"]`, args: []string{"--sort", "-.", "-o", "md"},
			stdout: "- 10\n- 9\n- null\n",
		},
		{
			// Arrays the filter yields more than once are sorted apart.
			name:  "sort shared arrays",
			stdin: `[3,1,2]`, args: []string{"--sort", ".top", "-o", "md", "{top: .[:2], all: .}"},
			stdout: "# top\n\n- 1\n- 3\n\n# all\n\n- 3\n- 1\n- 2\n",
		},
		{
			name:  "sort error",
			stdin: `{"a":1}`, args: []string{"--sort", ".a", "."},
			stdout: "╭───┬───╮\n│ a │ 1 │\n╰───┴───╯\n",
			stderr: "tq: --sort .a: .a is a number, not an array, and no array in the output has elements with .a\n",
			code:   5,
		},
		{
			name:  "short sort after slurp",
			stdin: `9 null "10"`, args: []string{"-sS", "-.", "-o", "md"},
			stdout: "- 10\n- 9\n- null\n",
		},
		{
			name:   "bad sort",
			args:   []string{"--sort", "--n"},
			stderr: "tq: --sort --n: one - is enough to sort in reverse\nRun 'tq --help' for usage.\n",
			code:   2,
		},
		{
			name:   "unknown style",
			args:   []string{"--style", "fancy"},
			stderr: "tq: unknown style: \"fancy\"\nRun 'tq --help' for usage.\n",
			code:   2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := tq(t, tc.stdin, tc.args...)
			if out != tc.stdout || errOut != tc.stderr || code != tc.code {
				t.Errorf("tq %q = (%d)\nstdout: %q\nstderr: %q\nwant (%d)\nstdout: %q\nstderr: %q",
					tc.args, code, out, errOut, tc.code, tc.stdout, tc.stderr)
			}
		})
	}
}

func TestInteractiveMode(t *testing.T) {
	// The viewer is only shown on a terminal: piped, the output is written
	// even where the viewer is the default.
	c := &cli{name: "tq", format: Table, mode: Interactive}
	out, errOut, code := run(t, c, `[1]`, ".")
	if want := "╭───╮\n│ 1 │\n╰───╯\n"; out != want || errOut != "" || code != 0 {
		t.Errorf("tq = (%d) %q %q, want %q", code, out, errOut, want)
	}
	for _, tc := range []struct {
		mode Mode
		want string
	}{
		{Print, "is a terminal; default: off)"},
		{Interactive, "is a terminal; default: on)"},
	} {
		c := &cli{name: "tq", format: Table, mode: tc.mode}
		if out, _, _ := run(t, c, "", "--help"); !strings.Contains(out, tc.want) {
			t.Errorf("help for mode %d doesn't contain %q:\n%s", tc.mode, tc.want, out)
		}
	}
	for _, args := range [][]string{{"-i"}, {"--no-interactive"}, {"-i", "--no-interactive"}} {
		opts, err := parseArgs(args)
		want := args[len(args)-1] == "-i"
		if err != nil || opts.interact == nil || *opts.interact != want {
			t.Errorf("parseArgs(%q).interact = %v, %v; want %v", args, opts.interact, err, want)
		}
	}
}

func TestSortFlags(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string // the sort keys as given
		err  string
	}{
		{args: []string{"--sort", ".id"}, want: ".id"},
		{args: []string{"--sort=-.id"}, want: "-.id"},
		{args: []string{"-S", ".id"}, want: ".id"},
		{args: []string{"-S-.id"}, want: "-.id"},
		{args: []string{"-S", "-.id", "-S.n"}, want: "-.id .n"},
		{args: []string{"-MS", "id", "."}, want: "id"},
		{args: []string{"-S"}, err: "-S needs 1 argument(s)"},
		{args: []string{"-S", "--id"}, err: "--sort --id: one - is enough to sort in reverse"},
	} {
		opts, err := parseArgs(tc.args)
		if tc.err != "" {
			if err == nil || err.Error() != tc.err {
				t.Errorf("parseArgs(%q): %v, want error %q", tc.args, err, tc.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseArgs(%q): %v", tc.args, err)
			continue
		}
		var got []string
		for _, k := range opts.sorts {
			got = append(got, k.spec)
		}
		if s := strings.Join(got, " "); s != tc.want {
			t.Errorf("parseArgs(%q) sorts by %q, want %q", tc.args, s, tc.want)
		}
	}
}

func TestKeyOrder(t *testing.T) {
	q, err := gojq.Parse(`{z, "lit": 1, $v, (.x): 2, "s\\(.y)": 3, a: {m: 1}}`)
	if err != nil {
		t.Fatal(err)
	}
	o := newKeyOrder()
	o.addQueryKeys(q) // keys whose names are computed are left out
	for _, k := range []string{"in1", "lit", "in2"} {
		o.add(k) // as read from the input
	}
	got := o.sort([]string{"other", "a", "in2", "m", "v", "z", "lit", "in1", "b"})
	want := "z lit v a m in1 in2 b other"
	if strings.Join(got, " ") != want {
		t.Errorf("sorted keys = %q, want %q", got, want)
	}
}
