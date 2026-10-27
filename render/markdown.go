package render

import (
	"io"
	"regexp"
	"strings"
	"unicode"
)

// maxHeading is the deepest heading level Markdown has.
const maxHeading = 6

// Markdown writes v to w as a Markdown document: objects become sections
// with a heading per key, arrays of objects and arrays of arrays become
// tables, other arrays of scalars become bullet lists, and scalars become
// paragraphs, or code blocks for multi-line text. Sections nested deeper
// than Markdown has heading levels get headings of the deepest level that
// show their whole path, like "a.b.c".
func Markdown(w io.Writer, v any, opts Options) error {
	r := &renderer{opts: opts}
	var b strings.Builder
	for i, blk := range r.doc(v) {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch {
		case blk.heading != nil:
			level := len(blk.heading)
			text := mdLine(headingText(blk.heading, level > maxHeading))
			if strings.HasSuffix(text, "#") {
				text = text[:len(text)-1] + `\#` // not a closing sequence
			}
			b.WriteString(strings.Repeat("#", min(level, maxHeading)))
			b.WriteString(" ")
			b.WriteString(text)
			b.WriteString("\n")
		case blk.code:
			fence := "```"
			for strings.Contains(strings.Join(blk.para.text, "\n"), fence) {
				fence += "`"
			}
			b.WriteString(fence)
			b.WriteString("\n")
			b.WriteString(strings.Join(blk.para.text, "\n"))
			b.WriteString("\n")
			b.WriteString(fence)
			b.WriteString("\n")
		case blk.para != nil:
			b.WriteString(mdLine(blk.para.text[0]))
			b.WriteString("\n")
		case blk.list != nil:
			for _, c := range blk.list {
				b.WriteString("- ")
				b.WriteString(mdStart(mdJoin(c.text)))
				b.WriteString("\n")
			}
		default:
			mdTable(&b, blk.header, blk.rows)
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// mdJoin escapes lines of text and joins them into one line with <br>
// tags, which Markdown shows as line breaks. Indentation is kept.
func mdJoin(lines []string) string {
	escaped := make([]string, len(lines))
	for i, l := range lines {
		escaped[i] = keepIndent(mdEscape(l))
	}
	return strings.Join(escaped, "<br>")
}

// keepIndent turns the spaces s starts with into ones Markdown shows.
func keepIndent(s string) string {
	trimmed := strings.TrimLeft(s, " ")
	return strings.Repeat("&nbsp;", len(s)-len(trimmed)) + trimmed
}

// mdTable writes a table with its columns padded to line up.
func mdTable(b *strings.Builder, header []string, rows [][]*cell) {
	lines := make([][]string, 0, len(rows)+1)
	lines = append(lines, make([]string, len(header)))
	for j, h := range header {
		lines[0][j] = mdEscape(h)
	}
	for _, row := range rows {
		line := make([]string, len(row))
		for j, c := range row {
			if !c.missing() {
				line[j] = mdJoin(c.text)
			}
		}
		lines = append(lines, line)
	}
	widths := make([]int, len(header))
	for _, line := range lines {
		for j, s := range line {
			widths[j] = max(widths[j], 3, width(s))
		}
	}
	right := func(j int) bool { return rows[0][j].right }
	write := func(line []string) {
		b.WriteString("|")
		for j, s := range line {
			pad := strings.Repeat(" ", widths[j]-width(s))
			if right(j) {
				s = pad + s
			} else {
				s += pad
			}
			b.WriteString(" ")
			b.WriteString(s)
			b.WriteString(" |")
		}
		b.WriteString("\n")
	}
	write(lines[0])
	b.WriteString("|")
	for j, w := range widths {
		b.WriteString(" ")
		if right(j) {
			b.WriteString(strings.Repeat("-", w-1))
			b.WriteString(": |")
		} else {
			b.WriteString(strings.Repeat("-", w))
			b.WriteString(" |")
		}
	}
	b.WriteString("\n")
	for _, line := range lines[1:] {
		write(line)
	}
}

// blockStart matches the start of a line that Markdown would take for a
// list item, a block quote, a heading, a thematic break or a code fence.
var blockStart = regexp.MustCompile(`^(?:[-+]|\d{1,9}[.)])(?: |$)|^[#>]|^--|^~~~`)

// mdLine escapes s for use as a line of its own, where its start must not
// be taken for the start of a block.
func mdLine(s string) string {
	return mdStart(mdEscape(s))
}

// mdStart escapes the start of s, Markdown text, so that it can't be taken
// for the start of a block.
func mdStart(s string) string {
	if m := blockStart.FindStringIndex(s); m != nil {
		i := m[1] - 1 // escape the last character of the marker
		if s[i] == ' ' {
			i--
		}
		s = s[:i] + `\` + s[i:]
	}
	return keepIndent(s)
}

// entity matches the start of an HTML character reference like "&amp;".
var entity = regexp.MustCompile(`^&#?[0-9A-Za-z]+;`)

// mdEscape escapes the characters in s that Markdown would otherwise take
// for formatting, so that s reads the same as plain text.
func mdEscape(s string) string {
	if !strings.ContainsAny(s, "\\`*_[]<|&") {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i, c := range rs {
		switch c {
		case '\\', '`', '*', '[', ']', '<', '|':
			b.WriteByte('\\')
		case '_':
			// Underscores inside words, as in snake_case, don't
			// emphasize.
			if i == 0 || i == len(rs)-1 || !isWord(rs[i-1]) || !isWord(rs[i+1]) {
				b.WriteByte('\\')
			}
		case '&':
			if entity.MatchString(string(rs[i:])) {
				b.WriteByte('\\')
			}
		}
		b.WriteRune(c)
	}
	return b.String()
}

func isWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
