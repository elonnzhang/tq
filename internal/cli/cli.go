// Package cli implements the tq and tqmd commands: jq that prints tables
// or Markdown for people to read instead of JSON.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/itchyny/gojq"
	"golang.org/x/term"

	"github.com/elonnzhang/tq/internal/tui"
	"github.com/elonnzhang/tq/render"
)

const version = "0.2.0"

// Exit codes, as in jq.
const (
	exitOK      = 0
	exitInput   = 2 // usage, file and JSON parse errors
	exitCompile = 3 // the filter doesn't compile
	exitRuntime = 5 // the filter failed on some input
)

// Output formats.
const (
	Table    = "table"
	Markdown = "md"
)

var formats = []string{Table, Markdown}

// A Mode is how output is shown on a terminal unless the command line
// says otherwise. Output that isn't to a terminal is always written.
type Mode int

const (
	Print       Mode = iota // write the output
	Interactive             // show the output in the full-screen viewer
)

// Main runs the command named name on the command line and standard
// streams of the process, showing results in format and mode unless the
// command line picks others, and returns the exit code.
func Main(name, format string, mode Mode) int {
	c := &cli{
		name:     name,
		format:   format,
		mode:     mode,
		stdin:    os.Stdin,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		stdinTTY: term.IsTerminal(int(os.Stdin.Fd())),
		environ:  os.Environ,
	}
	if fd := int(os.Stdout.Fd()); term.IsTerminal(fd) {
		c.tty = true
		if w, _, err := term.GetSize(fd); err == nil {
			c.width = w
		}
		c.color = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	}
	if c.width == 0 {
		if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 0 {
			c.width = n
		}
	}
	return c.run(os.Args[1:])
}

type cli struct {
	name           string // command name, for messages
	format         string // output format unless an option picks one
	mode           Mode   // how to show output unless an option picks one
	stdin          io.Reader
	stdout, stderr io.Writer
	stdinTTY       bool
	environ        func() []string
	// Defaults for when no option sets them, based on the terminal.
	tty   bool // whether stdout is one, where Markdown is drawn, not written
	width int  // zero means unlimited
	color bool
}

func (c *cli) run(args []string) int {
	opts, err := parseArgs(args)
	if err != nil {
		return c.usageError(err)
	}
	switch {
	case opts.help:
		fmt.Fprint(c.stdout, usage(c.name, c.format, c.mode))
		return exitOK
	case opts.version:
		fmt.Fprintln(c.stdout, c.name, version)
		return exitOK
	case opts.filter == "" && len(opts.files) == 0 && c.stdinTTY && !opts.nullInput:
		// Nothing to read but a terminal: the user probably wants help.
		fmt.Fprint(c.stderr, usage(c.name, c.format, c.mode))
		return exitInput
	}
	style, ok := render.Styles[opts.style]
	if !ok {
		return c.usageError(fmt.Errorf("unknown style: %q", opts.style))
	}
	format := c.format
	if opts.output != "" {
		format = opts.output
	}
	if !slices.Contains(formats, format) {
		return c.usageError(fmt.Errorf("unknown output format: %q", format))
	}
	if opts.filter == "" {
		opts.filter = "."
	}
	interact := c.mode == Interactive
	if opts.interact != nil {
		interact = *opts.interact
	}

	query, err := gojq.Parse(opts.filter)
	if err != nil {
		c.errorf("invalid filter: %v", err)
		return exitCompile
	}
	order := newKeyOrder()
	order.addQueryKeys(query)
	in := newInputs(opts.files, c.stdin, order)
	var iter gojq.Iter = in
	if opts.slurp {
		iter = slurp(in)
	}
	names := append(opts.names, "$ARGS")
	values := append(opts.values, map[string]any{"named": opts.named, "positional": []any{}})
	code, err := gojq.Compile(query,
		gojq.WithVariables(names),
		gojq.WithEnvironLoader(c.environ),
		gojq.WithInputIter(iter),
	)
	if err != nil {
		c.errorf("invalid filter: %v", err)
		return exitCompile
	}

	var results []any
	status := exitOK
	runOn := func(v any) (halted bool) {
		out := code.Run(v, values...)
		for {
			v, ok := out.Next()
			if !ok {
				return false
			}
			if err, ok := v.(error); ok {
				var halt *gojq.HaltError
				if errors.As(err, &halt) {
					status = c.halt(halt)
					return true
				}
				c.errorf("error: %s", strings.TrimPrefix(err.Error(), "error: "))
				status = exitRuntime
				return false // jq goes on with the next input
			}
			results = append(results, v)
		}
	}
	if opts.nullInput {
		runOn(nil)
	} else {
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, ok := v.(error); ok {
				c.errorf("%v", err)
				continue
			}
			if runOn(v) {
				break
			}
		}
	}
	if in.failed && status == exitOK {
		status = exitInput
	}

	if len(results) > 0 {
		var v any = results
		if len(results) == 1 {
			v = results[0]
		}
		if len(opts.sorts) > 0 {
			v = clone(v)
		}
		// Sort by the last key first, so that the sorts that follow keep
		// the order of the keys before for ties.
		for _, k := range slices.Backward(opts.sorts) {
			if err := sortBy(v, k); err != nil {
				c.errorf("--sort %s: %v", k.spec, err)
				status = exitRuntime
			}
		}
		ropts := render.Options{
			Width:  c.width,
			Color:  c.color,
			Style:  style,
			Expand: opts.expand,
			Keys:   order.keys,
		}
		if opts.color != nil {
			ropts.Color = *opts.color
		}
		if opts.width >= 0 {
			ropts.Width = opts.width
		}
		var err error
		switch {
		case interact && c.tty:
			err = c.interact(v, format, ropts, opts.width)
		case format == Table:
			err = render.Render(c.stdout, v, ropts)
		case c.tty:
			err = render.Doc(c.stdout, v, ropts)
		default:
			err = render.Markdown(c.stdout, v, ropts)
		}
		if err != nil {
			c.errorf("%v", err)
			return exitInput
		}
	}
	return status
}

// interact shows v in the full-screen viewer, drawn in format as a table
// or document. The viewer starts at the natural width of the output, or at
// width if that is set, and can fit it to the screen instead.
func (c *cli) interact(v any, format string, opts render.Options, width int) error {
	return tui.Show(func(limit int) ([]string, int) {
		opts.Width = max(width, 0)
		if limit > 0 {
			opts.Width = limit
		}
		page := render.DocPage
		if format == Table {
			page = render.TablePage
		}
		p := page(v, opts)
		return p.Lines, p.Head
	}, false, opts.Color)
}

// halt reports the value of a halt_error to stderr the way jq does and
// returns the exit code it asks for.
func (c *cli) halt(err *gojq.HaltError) int {
	switch v := err.Value().(type) {
	case nil:
	case string:
		fmt.Fprint(c.stderr, v)
	default:
		b, _ := gojq.Marshal(v)
		fmt.Fprintf(c.stderr, "%s\n", b)
	}
	return err.ExitCode()
}

func (c *cli) errorf(format string, args ...any) {
	fmt.Fprintf(c.stderr, "%s: %s\n", c.name, fmt.Sprintf(format, args...))
}

func (c *cli) usageError(err error) int {
	c.errorf("%v", err)
	fmt.Fprintf(c.stderr, "Run '%s --help' for usage.\n", c.name)
	return exitInput
}

// slurped yields all the values of an iterator in one array, or the first
// error it meets. It reads nothing until asked, so that -ns doesn't wait
// for stdin unless the filter calls input.
type slurped struct {
	iter gojq.Iter
	done bool
}

func slurp(iter gojq.Iter) gojq.Iter {
	return &slurped{iter: iter}
}

func (s *slurped) Next() (any, bool) {
	if s.done {
		return nil, false
	}
	s.done = true
	all := []any{}
	for {
		v, ok := s.iter.Next()
		if !ok {
			return all, true
		}
		if _, ok := v.(error); ok {
			return v, true
		}
		all = append(all, v)
	}
}
