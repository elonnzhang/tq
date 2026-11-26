package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// isTextTable reports whether a line looks like whitespace-separated table
// fields rather than the beginning of JSON or a stream of JSON values.
func isTextTable(line string) bool {
	line = strings.TrimSpace(ansi.Strip(line))
	if line == "" || strings.ContainsRune("{[\"", rune(line[0])) {
		return false
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return false
	}
	// A JSON first field keeps even a malformed stream on the JSON path,
	// where later invalid fields receive the usual parse error.
	return !json.Valid([]byte(fields[0]))
}

// A tab or at least two spaces can separate columns while leaving single
// spaces inside cells. This split is used only when every row fits the header.
var textTableSeparator = regexp.MustCompile(`[ \t]{2,}|\t`)

// readTextTable reads a complete table as one array. Uppercase ASCII headers
// make object rows; tables without headers make arrays of string fields.
func readTextTable(r io.Reader, order *keyOrder) (any, error) {
	type line struct {
		text   string
		number int
	}
	var lines []line
	reader := bufio.NewReader(r)
	for number := 1; ; number++ {
		text, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("text table: %w", err)
		}
		text = strings.TrimSpace(ansi.Strip(text))
		if text != "" {
			lines = append(lines, line{text, number})
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	rows := []any{}
	if len(lines) == 0 {
		return rows, nil
	}
	fields := strings.Fields(lines[0].text)
	width := len(fields)
	header := textTableHeader(fields)
	split := strings.Fields
	if header && len(textTableSeparator.Split(lines[0].text, -1)) == width {
		aligned := true
		for _, l := range lines[1:] {
			if len(textTableSeparator.Split(l.text, -1)) != width {
				aligned = false
				break
			}
		}
		if aligned {
			split = func(s string) []string { return textTableSeparator.Split(s, -1) }
		}
	}
	if header {
		seen := map[string]bool{}
		for _, key := range fields {
			if seen[key] {
				return nil, fmt.Errorf("text table: line %d: duplicate header %q", lines[0].number, key)
			}
			seen[key] = true
			if order != nil {
				order.add(key)
			}
		}
		lines = lines[1:]
	}
	for _, l := range lines {
		values := split(l.text)
		if len(values) != width {
			return nil, fmt.Errorf("text table: line %d: expected %d columns, got %d", l.number, width, len(values))
		}
		if header {
			row := make(map[string]any, width)
			for i, key := range fields {
				row[key] = values[i]
			}
			if order != nil {
				order.record(row, fields)
			}
			rows = append(rows, row)
		} else {
			row := make([]any, width)
			for i, value := range values {
				row[i] = value
			}
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func textTableHeader(fields []string) bool {
	for _, field := range fields {
		letter := false
		for _, c := range field {
			if c >= 'A' && c <= 'Z' {
				letter = true
			} else if !(c >= '0' && c <= '9' || c == '_' || c == '-') {
				return false
			}
		}
		if !letter {
			return false
		}
	}
	return len(fields) > 0
}
