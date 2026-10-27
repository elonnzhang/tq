# tq

tq is jq for people. It runs a jq filter on JSON and draws the results as tables instead of printing JSON. Its sibling `tqmd` draws them as a Markdown document.

```console
$ herdr agent list | tq '.result.agents[] | {pane_id, agent, status: .agent_status, cwd}'
╭─────────┬────────┬─────────┬───────────────────╮
│ pane_id │ agent  │ status  │ cwd               │
├─────────┼────────┼─────────┼───────────────────┤
│ w5:pJ   │ pi     │ idle    │ /home/dev/src/api │
│ w5:p12  │ claude │ working │ /home/dev/src/tq  │
│ w13:p2  │ claude │ done    │ /home/dev/src/bot │
╰─────────┴────────┴─────────┴───────────────────╯
```

Filters are jq's, run by [gojq](https://github.com/itchyny/gojq). The command line follows jq too: the filter comes first, then the files to read. With no files, tq reads stdin. The filter defaults to `.`, but it must be given when files are, as in `tq . data.json`.

## Install

Download the archive for your system from the [releases](https://github.com/elonnzhang/tq/releases), and put `tq` and `tqmd` from it somewhere on your `PATH`. There are builds for macOS, Linux and Windows, on amd64 and arm64.

With Go 1.26 or later, you can install them from source instead:

```sh
go install github.com/elonnzhang/tq@latest github.com/elonnzhang/tq/cmd/tqmd@latest
```

In a checkout, `go install ./...` does the same.

`tqmd` is `tq -o md` under its own name, except that on a terminal it opens [the viewer](#the-viewer) by default.

## Tables

| Value            | Drawn as                       |
| ---------------- | ------------------------------ |
| object           | a table of keys and values     |
| array of objects | a table with a column per key  |
| array of arrays  | a table with a row per array   |
| other array      | a table with a row per item    |
| scalar           | plain text                     |

When the filter yields several results, they are drawn together as one array, so `.items[] | {name, size}` makes one table. Columns come in the order the filter writes the keys, then in input order.

How a nested value is drawn depends on where it is:

- as a value in a table of keys and values, it is drawn as a table of its own;
- in the rows of an array, it is written as `- key: value` lines, and an array of scalars as `a, b`.

`-x` draws nested values in rows as tables too.

```console
$ tq '.result.agents[] | {pane_id, agent, session: (.agent_session | {kind, source})}'
╭─────────┬────────┬────────────────────────╮
│ pane_id │ agent  │ session                │
├─────────┼────────┼────────────────────────┤
│ w5:pJ   │ pi     │ - kind: path           │
│         │        │ - source: herdr:pi     │
├─────────┼────────┼────────────────────────┤
│ w5:p12  │ claude │ - kind: id             │
│         │        │ - source: herdr:claude │
├─────────┼────────┼────────────────────────┤
│ w13:p2  │ claude │ - kind: id             │
│         │        │ - source: herdr:claude │
╰─────────┴────────┴────────────────────────╯
```
Tables are kept within the line width: the terminal's width, or `N` with `-w N`. CJK characters count as two. Piped output has no limit unless `COLUMNS` is set, and `-w 0` turns off the limit everywhere.

Text too long for its column wraps onto more lines rather than being cut, and the room goes to the columns whose text needs it most. If an array's columns can't fit side by side, each element is drawn as a record instead:

```console
$ tq -w 44 '.result.agents[:2] | map({pane_id, agent, agent_status, cwd, focused})'
╭──────────────┬───────────────────╮
│ pane_id      │ w5:pJ             │
│ agent        │ pi                │
│ agent_status │ idle              │
│ cwd          │ /home/dev/src/api │
│ focused      │ false             │
├──────────────┼───────────────────┤
│ pane_id      │ w5:p12            │
│ agent        │ claude            │
│ agent_status │ working           │
│ cwd          │ /home/dev/src/tq  │
│ focused      │ true              │
╰──────────────┴───────────────────╯
```

## Markdown

`tqmd`, or `tq -o md`, lays the output out as a document:

| Value            | Written as                                                                                                       |
| ---------------- | ---------------------------------------------------------------------------------------------------------------- |
| object           | a section headed by each key, with nested objects one heading level deeper                                       |
| array of objects | a table with a column per key; nested objects get columns like `session.kind`, as long as there are at most 5 columns, and are written as `- key: value` lines otherwise |
| array of arrays  | a table with a column per index                                                                                  |
| other array      | a bullet list, or a section per item for objects and arrays                                                      |
| scalar           | a paragraph                                                                                                      |

Piped, tqmd writes Markdown text:

```console
$ tqmd '{agents: [.result.agents[] | {pane_id, agent, session: (.agent_session | {kind, source})}]}' | cat
# agents

| pane_id | agent  | session.kind | session.source |
| ------- | ------ | ------------ | -------------- |
| w5:pJ   | pi     | path         | herdr:pi       |
| w5:p12  | claude | id           | herdr:claude   |
| w13:p2  | claude | id           | herdr:claude   |
```

On a terminal, tqmd draws the document instead. Each section is indented under its heading, and tables get borders.
## The viewer

`tq -i` shows the output in a full-screen viewer, and so does tqmd unless given `--no-interactive`. The viewer only opens on a terminal: piped or redirected, the output is written out as text.

The viewer scrolls both ways, and the header of a table stays in view as it scrolls. Tables are drawn at full width unless `-w` is given.

| Keys                    | Action                                       |
| ----------------------- | -------------------------------------------- |
| `j` `k`, `↓` `↑`        | scroll a line                                |
| `space` `b`, PgDn PgUp  | scroll a screen                              |
| `d` `u`                 | scroll half a screen                         |
| `g` `G`, Home End       | go to the top or the bottom                  |
| `h` `l`, `←` `→`        | scroll 8 columns sideways                    |
| `H` `L`                 | scroll half a screen sideways                |
| `0` `$`                 | go to the left or the right edge             |
| `w`                     | switch between full width and fit to screen  |
| `c`                     | copy the output as text                      |
| `q`, Esc                | quit                                         |

- **Mouse:** the wheel scrolls, and holding shift scrolls sideways. The scroll bars can be clicked and dragged.
- **Copying:** the viewer takes over the mouse, so copy with `c` rather than by selecting text. `c` uses the first of pbcopy, clip.exe, wl-copy, xclip and xsel that it finds. If there is none, as over SSH, it copies through the terminal with OSC 52.

## Sorting

`-S PATH`, or `--sort PATH`, sorts arrays in the output of the filter by the value at PATH in their elements:

```sh
tq -S .data.created          # sort .data by .created; same as .data[].created
tq -S .created               # sort the output if its elements have .created,
                             # or else every array in it whose elements do
tq -S -.created              # in reverse
tq -S .team -S -.created     # by team, then newest first
tq -S '.rows[0]'             # sort rows of arrays by their first column
tq -S .                      # sort an array of scalars by the values themselves
```

The leading dot may be left out. Keys that aren't plain identifiers are quoted as in jq, as in `."owned by"`.

PATH is a path in what the filter outputs, not in its input. For example, with the filter `'.data | map({id})'`, sort by `.id`, not `.data.id`.

How values compare:

- Numbers sort by value, and so do strings of numbers, so `"9"` comes before `"10"`.
- Other text sorts naturally: runs of digits compare as numbers, so `v1.9 < v1.10 < V2 < v10`. Case is ignored except to break ties.
- Nulls and missing values go last, in either direction.

```console
$ echo '{"data":[{"id":"b","n":"10"},{"id":"a","n":9},{"id":"c"}]}' | tq -S -.data.n
╭──────┬─────────────╮
│ data │ ╭────┬────╮ │
│      │ │ id │ n  │ │
│      │ ├────┼────┤ │
│      │ │ b  │ 10 │ │
│      │ │ a  │ 9  │ │
│      │ │ c  │    │ │
│      │ ╰────┴────╯ │
╰──────┴─────────────╯
```
## Options

| Option                         | Meaning                                                         |
| ------------------------------ | --------------------------------------------------------------- |
| `-o table`, `-o md`            | output format; tq defaults to `table`, tqmd to `md`             |
| `-i`, `--no-interactive`       | show the output in the viewer, or write it                      |
| `-n`                           | use `null` as the only input                                    |
| `-s`                           | read all inputs into one array and run the filter on it once    |
| `-x`                           | draw nested values in table rows as tables                      |
| `-w N`                         | keep lines at most N characters wide; 0 means no limit          |
| `-S PATH`                      | sort, as above; repeat it to break ties                         |
| `--style ascii`                | draw borders with `+-\|` instead of rounded box lines           |
| `-C`, `-M`                     | turn colors on or off                                           |
| `--arg NAME VALUE`, `--argjson NAME JSON` | set `$NAME`, as in jq                                |

Colors are on by default on a terminal, unless `NO_COLOR` is set or `TERM` is `dumb`. Short flags combine as in jq, so `-sS .n` and `-w80` both work. `tq --help` gives the details.

## Differences from jq

- Output is always drawn, never JSON. `-r` is accepted but does nothing, since strings are never quoted.
- `-S` means `--sort`, not jq's `--sort-keys`.
- All the results of a run are drawn together, as one array.
- Exit codes are jq's:
  - 2 for usage, file and JSON errors;
  - 3 when the filter doesn't compile;
  - 5 when it fails on some input;
  - the code `halt_error` asks for.

## Development

```sh
gofmt -l . ; go vet ./... && go test ./...
go run . -S .pane_id '.result.agents | map({pane_id, agent})' internal/cli/testdata/herdr-agent-list.json
```

`go run .` runs tq, and `go run ./cmd/tqmd` runs tqmd. [AGENTS.md](AGENTS.md) describes how the code is laid out.

### Releasing

1. Set `version` in `internal/cli/cli.go` to the new version, say `0.3.0`, and commit it.
2. Tag the commit and push the tag:

   ```sh
   git tag -a v0.3.0 -m "tq 0.3.0" && git push origin v0.3.0
   ```

The [release workflow](.github/workflows/release.yml) then tests tq on Go 1.26 and on the latest Go. It fails if the tag isn't the version tq prints. Otherwise it builds the archives with `scripts/dist.sh` and publishes the release.

To try the build without publishing, run `scripts/dist.sh`, which writes the archives to `dist/`. Or run the workflow by hand from the Actions tab, or with `gh workflow run release.yml`: it then keeps the archives as an artifact of the run.
