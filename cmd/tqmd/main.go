// Command tqmd is jq that prints Markdown for people to read instead of
// JSON. It is tq with Markdown output by default, the same as "tq -o md",
// and on a terminal it shows the document in the full-screen viewer.
package main

import (
	"os"

	"github.com/elonnzhang/tq/internal/cli"
)

func main() {
	os.Exit(cli.Main("tqmd", cli.Markdown, cli.Interactive))
}
