# AGENTS.md

This file provides guidance to coding agents such as Claude Code (claude.ai/code) | ChatGPT Codex (chatgpt.com/codex) when working with code in this repository.

## What this is

tq is jq (through gojq) that draws its results as tables or Markdown for people to read instead of JSON. Two commands share `cli.Main(name, format, mode)`:

- `main.go` (the module root, `github.com/elonnzhang/tq`): tq, which writes its output; `-i` opens the full-screen viewer.
- `cmd/tqmd`: tq with Markdown output by default (`tq -o md`), which opens the viewer by default on a terminal. With `--no-interactive`, it draws an indented document on a terminal and writes raw Markdown when piped.

Piped output is always written, whatever the default. `cmd/tq` holds only a `.gitkeep`. Don't put a main package there: it would build a second binary named `tq`, and `go install ./...` would then install either one at random.

## Commands

```sh
gofmt -l . ; go vet ./... && go test ./...
go test ./render -run TestShrink                 # one test
go test ./internal/cli -run 'TestCLI/sort_error'  # one subtest (spaces in names become _)
GOOS=windows go build ./... && GOOS=linux go build ./...   # the viewer must cross-compile

echo '[{"id":"v10"},{"id":"v2"}]' | go run . --sort .id
go run . '.result.agents[] | {pane_id, cwd}' internal/cli/testdata/herdr-agent-list.json
```

As in jq, the first positional argument is always the filter, so pass `.` before any file names.

To check the viewer (`go run . -i`, or `go run ./cmd/tqmd`) without a person at the keyboard, run it in a detached tmux session and read the screen back:

- start it with `tmux new-session -d -x 90 -y 14 "<cmd>"`;
- read the screen with `capture-pane -p` (add `-e` to keep colors);
- send keys with `send-keys`, and mouse events as SGR sequences with `send-keys -l`, e.g. `\e[<65;X;YM` for the wheel scrolling down.

## How a run goes (`internal/cli/cli.go`, `run`)

1. **Parse the command line.** `parseArgs` in `args.go` follows jq:
   - short flags combine, as in `-sMw80`, and `-w`, `-o` and `-S` take the rest of their cluster as their value;
   - options may come after the filter, and `--` ends them.

   The help text is `usage()` in the same file. Update it with every change to the options, keeping lines under 80 columns.
2. **Rank the filter's keys.** The filter is parsed with gojq, and `keyOrder.addQueryKeys` (`order.go`) ranks the object keys the filter writes, in the order written.
3. **Read the inputs.** `inputs` (`input.go`) decodes the JSON stream with `UseNumber`. Gojq keeps objects as Go maps, which lose key order, so `inputs` records the key order of every object it reads.
   - Display order of keys: the filter's keys, then input order, then sorted.
   - Numbers reach later code as `json.Number`, or as `int`, `float64` or `*big.Int` from gojq arithmetic. Code that handles numbers must accept all four.
4. **Run the filter on each input.** Results are collected. One result is drawn as itself; several are drawn as one array. A runtime error is reported and the run goes on with the next input, as in jq.
   - Exit codes follow jq: 2 for usage or input errors, 3 when the filter doesn't compile, 5 for runtime errors.
5. **Apply `--sort`/`-S`** (`sort.go`). It works on a deep copy of the results, because gojq slices share their backing arrays. Keys are applied last first, with a stable sort, so that earlier keys win ties.
6. **Draw.** The output goes through one of:
   - `render.Render` for tables;
   - `render.Doc` for Markdown drawn on a terminal;
   - `render.Markdown` when Markdown is piped;
   - `tui.Show`, given a `Draw` closure that draws a `render.TablePage` or `render.DocPage` again at whatever width the viewer asks for.

## render

- **`render.go`** turns a value into a `table` of `cell`s; a cell is either text or a nested value.
  - Shapes: an object becomes a key/value table, an array of objects gets a column per key, an array of arrays gets a row per array, and any other array becomes one column.
  - Nested values inside rows are written as `- key: value` lines (`inlineCell`, in `doc.go`), unless `-x` asks for nested tables.
- **`layout.go`** fits tables to the width.
  - `shrink` starts each column at `min(natural, minColumn)`, then gives the spare cells to the columns where they save the most wrapped lines.
  - An array whose columns can't get narrow enough (`narrowest(widths) > limit`) is drawn as one record per element instead (`vertical`).
  - `draw` also returns how many lines are header, which the viewer keeps in view while scrolling.
- **`doc.go`** builds the document model (`docBlock`s) that both Markdown output (`markdown.go`) and the terminal document view (`docview.go`) draw. In document tables, the fields of nested objects get `a.b` columns, as long as the table stays within `maxFlatColumns` (5).
- **`text.go`** measures widths with go-runewidth, counting East Asian ambiguous characters as one cell.
  - `clean` turns control characters into visible symbols. All text taken from input, keys and strings alike, must go through it before it is drawn, so that input can't inject escape sequences.

## internal/tui

- **`view.go`**: the `viewer` keeps all the viewer's state and logic (layout, scrolling, scroll bars, the status line, keys and mouse dragging) with no terminal attached, so it can be unit-tested.
- **`terminal.go`**: adapts Bubble Tea v2 (`charm.land/bubbletea/v2`) messages to the viewer. It uses the alt screen and cell-motion mouse mode.
- **`text.go`**: cuts colored lines by grapheme cluster (uax29). It must measure widths the same way package render does.
- **`clipboard.go`**: copies with pbcopy, clip.exe, wl-copy, xclip or xsel, and falls back to OSC 52.

## Conventions

- The output formats were agreed with the maintainer:
  - nested values in table cells are `- key: value` lines, and arrays of scalars are `a, b`;
  - cells wrap and are never cut, and column widths follow their content. The one exception: a nested table that can't get narrow enough for its column is cut, ending in `…`.

  Show a sample of the new output before changing what any output looks like.
- `-w` and `Options.Width` count terminal cells, with CJK characters as two. They are not a number of table columns. 0 means no limit.
- Doc comments are plain sentences about behavior, in the style of the Go standard library. Match their density and voice.
- CLI tests run `cli.run` on given stdin and compare stdout, stderr and the exit code exactly, through the `tq(t, stdin, args...)` helper in `cli_test.go`. Render tests compare the drawn text exactly.
- `internal/cli/testdata/herdr-agent-list.json` is sanitized. Don't replace it with real output that contains local paths.
- Releases come from `.github/workflows/release.yml`, which runs on a pushed `v*` tag. The tag must match `version` in `internal/cli/cli.go`, or the workflow fails. The archives are built by `scripts/dist.sh`, which can be run locally and writes to `dist/`. Actions in the workflow are pinned to commit SHAs, with the version in a comment; keep them pinned when updating them.
