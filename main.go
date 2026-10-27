// Command tq is jq that prints tables for people to read instead of JSON.
// With -i it shows them in a full-screen viewer instead.
package main

import (
	"os"

	"github.com/elonnzhang/tq/internal/cli"
)

func main() {
	os.Exit(cli.Main("tq", cli.Table, cli.Print))
}
