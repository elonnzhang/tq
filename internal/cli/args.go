package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// usage returns the help for the command named name, which shows output in
// format and mode unless options pick others.
func usage(name, format string, mode Mode) string {
	viewer := "off"
	if mode == Interactive {
		viewer = "on"
	}
	return fmt.Sprintf(`Usage: %[1]s [options] [filter] [file...]

%[1]s is jq that prints tables or Markdown for people to read instead of
JSON. It runs the jq filter (default ".") on each JSON value read from the
files, or stdin when there are none, and draws the results.

Tables (-o table):
  object            a table of keys and values
  array of objects  a table with a column per key
  array of arrays   a table with a row per array
  other array       a table with a row per item
  scalar            plain text
Objects and arrays inside table rows are written as "- key: value" lines.
Text too long for its column wraps onto more lines. Arrays whose columns
don't fit side by side in the line width are drawn as a list of records
instead.

Markdown (-o md):
  object            a section headed by each key, with nested objects one
                    heading level deeper
  array of objects  a table with a column per key; the fields of nested
                    objects get columns like "session.kind", or are written
                    as "- key: value" lines if that makes more than 5
                    columns
  array of arrays   a table with a column per index
  other array       a bullet list, or a section per item for objects and
                    arrays
  scalar            a paragraph
On a terminal the document is drawn with each section indented under its
heading; elsewhere it is written as Markdown text, as in '%[1]s -o md | cat'.

When the filter yields several results, they are drawn together as if
collected into an array, so '.items[] | {name, size}' makes one table.
Columns and sections appear in the order the filter writes them, then in
input order.

Options:
  -o, --output FORMAT      output format: table or md (default: %[2]s)
  -i, --interactive        show the output in a full-screen viewer that
                           scrolls both ways, with tables at full width
                           unless -w is given; the mouse wheel scrolls, and
                           sideways with shift held, the scroll bars can be
                           dragged, w fits tables to the screen, c copies
                           the output as text, q quits (only when output
                           is a terminal; default: %[3]s)
      --no-interactive     write the output instead of showing the viewer
  -n, --null-input         use null as the single input value
  -s, --slurp              read all inputs into an array and use it as the
                           single input value
  -x, --expand             draw objects and arrays inside table rows as
                           nested tables instead of "- key: value" lines
  -w, --width N            keep lines at most N characters wide, counting
                           CJK characters as two (default: terminal width;
                           0 means no limit)
  -S, --sort PATH          sort arrays in the output of the filter by PATH
                           in their elements: '--sort .data.created' sorts
                           .data by .created, as does '.data[].created'.
                           Without the path to the array, as in
                           '.created', the output is sorted if its
                           elements have PATH, or else every array in it
                           whose elements do. Numbers sort by value, and
                           so do digits in text: "v2" before "v10". Nulls
                           and missing values go last. A leading - sorts
                           in reverse: '--sort -.created'. Repeat --sort
                           to break ties
      --style NAME         border style: rounded (default) or ascii
  -C, --color-output       colorize output
  -M, --monochrome-output  don't colorize output
      --arg NAME VALUE     set $NAME to the string VALUE
      --argjson NAME JSON  set $NAME to the JSON value
  -r, --raw-output         ignored; strings are always shown without quotes
  -h, --help               show this help
      --version            show the version

Examples:
  herdr agent list | %[1]s
  herdr agent list | %[1]s '.result.agents[] | {pane_id, agent, agent_status, cwd}'
  %[1]s -s 'map({name, size})' a.json b.json
`, name, format, viewer)
}

// valued are the long options that take a value, which may be written
// "--name=value".
var valued = []string{"--width", "--style", "--output", "--sort"}

// shortValued are the short flags that take a value, which may be written
// right after them in a cluster, as in "-w80".
const shortValued = "woS"

// options are the parsed command line.
type options struct {
	filter    string
	files     []string
	output    string // empty means the command's default
	nullInput bool
	slurp     bool
	expand    bool
	interact  *bool // nil means the command's default
	width     int   // -1 means the terminal width
	style     string
	color     *bool // nil means automatic
	sorts     []sortKey
	names     []string
	values    []any
	named     map[string]any // $ARGS.named
	help      bool
	version   bool
}

// parseArgs parses the command line the way jq does: short flags may be
// combined, options may come after the filter, and "--" ends them.
func parseArgs(args []string) (*options, error) {
	o := &options{width: -1, style: "rounded", named: map[string]any{}}
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}
		// take returns the next n arguments as the values of flag.
		take := func(flag string, n int) ([]string, error) {
			if i+n >= len(args) {
				return nil, fmt.Errorf("%s needs %d argument(s)", flag, n)
			}
			vals := args[i+1 : i+1+n]
			i += n
			return vals, nil
		}
		if strings.HasPrefix(arg, "--") {
			name, val, hasVal := strings.Cut(arg, "=")
			if hasVal && !slices.Contains(valued, name) {
				return nil, fmt.Errorf("unknown option: %s", arg)
			}
			if !hasVal && slices.Contains(valued, name) {
				vals, err := take(name, 1)
				if err != nil {
					return nil, err
				}
				val = vals[0]
			}
			if err := o.long(name, val, take); err != nil {
				return nil, err
			}
			continue
		}
		// A cluster of short flags like -ns or -w80.
		for j := 1; j < len(arg); j++ {
			if err := o.short(arg[j], arg[j+1:], take); err != nil {
				return nil, err
			}
			if strings.IndexByte(shortValued, arg[j]) >= 0 {
				break // the rest of the cluster was its value, if any
			}
		}
	}
	if len(positional) > 0 {
		o.filter, o.files = positional[0], positional[1:]
	}
	return o, nil
}

type taker func(flag string, n int) ([]string, error)

func (o *options) long(name, val string, take taker) error {
	switch name {
	case "--null-input":
		o.nullInput = true
	case "--slurp":
		o.slurp = true
	case "--expand":
		o.expand = true
	case "--interactive":
		o.setInteract(true)
	case "--no-interactive":
		o.setInteract(false)
	case "--width":
		return o.setWidth(val)
	case "--style":
		o.style = val
	case "--output":
		o.output = val
	case "--sort":
		return o.addSort(val)
	case "--color-output":
		o.setColor(true)
	case "--monochrome-output":
		o.setColor(false)
	case "--raw-output":
	case "--help":
		o.help = true
	case "--version":
		o.version = true
	case "--arg", "--argjson":
		vals, err := take(name, 2)
		if err != nil {
			return err
		}
		var v any = vals[1]
		if name == "--argjson" {
			if v, err = parseJSON(vals[1]); err != nil {
				return fmt.Errorf("--argjson %s: invalid JSON: %v", vals[0], err)
			}
		}
		o.names = append(o.names, "$"+vals[0])
		o.values = append(o.values, v)
		o.named[vals[0]] = v
	default:
		return fmt.Errorf("unknown option: %s", name)
	}
	return nil
}

// short handles the short flag c, rest being what follows it in its
// cluster.
func (o *options) short(c byte, rest string, take taker) error {
	switch c {
	case 'n':
		o.nullInput = true
	case 's':
		o.slurp = true
	case 'x':
		o.expand = true
	case 'i':
		o.setInteract(true)
	case 'C':
		o.setColor(true)
	case 'M':
		o.setColor(false)
	case 'r':
	case 'h':
		o.help = true
	case 'w', 'o', 'S':
		if rest == "" {
			vals, err := take("-"+string(c), 1)
			if err != nil {
				return err
			}
			rest = vals[0]
		}
		switch c {
		case 'o':
			o.output = rest
		case 'S':
			return o.addSort(rest)
		default:
			return o.setWidth(rest)
		}
	default:
		return fmt.Errorf("unknown option: -%c", c)
	}
	return nil
}

func (o *options) addSort(spec string) error {
	k, err := parseSortKey(spec)
	if err != nil {
		return fmt.Errorf("--sort %s: %v", spec, err)
	}
	o.sorts = append(o.sorts, k)
	return nil
}

func (o *options) setWidth(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return fmt.Errorf("invalid width: %q", s)
	}
	o.width = n
	return nil
}

func (o *options) setColor(on bool) {
	o.color = &on
}

func (o *options) setInteract(on bool) {
	o.interact = &on
}

// parseJSON parses s as a single JSON value.
func parseJSON(s string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, fmt.Errorf("unexpected data after the value")
	}
	return v, nil
}
