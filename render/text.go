package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mattn/go-runewidth"
)

// cond measures text in terminal cells. East Asian ambiguous characters are
// counted as one cell regardless of locale: the box-drawing characters we
// emit are ambiguous, and nearly every terminal draws them one cell wide.
var cond = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

const ellipsis = "…"

// width returns the display width of s, ignoring ANSI escape sequences.
func width(s string) int {
	if strings.IndexByte(s, 0x1b) >= 0 {
		s = stripANSI(s)
	}
	return cond.StringWidth(s)
}

func maxWidth(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, width(l))
	}
	return w
}

// truncate shortens s to at most w cells, marking the cut with an ellipsis.
// Colors are dropped from lines that need cutting.
func truncate(s string, w int) string {
	if width(s) <= w {
		return s
	}
	if w <= 0 {
		return ""
	}
	return cond.Truncate(stripANSI(s), w, ellipsis)
}

// wrap breaks plain text s into lines of at most w cells, between words
// where it can. Lines after the first start under the text of the first,
// past its indentation and any "- " list marker.
func wrap(s string, w int) []string {
	if cond.StringWidth(s) <= w {
		return []string{s}
	}
	n := len(s) - len(strings.TrimLeft(s, " "))
	if strings.HasPrefix(s[n:], "- ") {
		n += 2
	}
	if n > w/2 {
		n = 0
	}
	pad := strings.Repeat(" ", n)
	var lines []string
	line, lineWidth := s[:n], n
	for _, word := range strings.SplitAfter(s[n:], " ") {
		// Spaces after a word may hang past the end of a line.
		text := strings.TrimRight(word, " ")
		spaces := len(word) - len(text)
		ww := cond.StringWidth(text)
		if lineWidth+ww > w && lineWidth > n {
			lines = append(lines, strings.TrimRight(line, " "))
			line, lineWidth = pad, n
		}
		if lineWidth+ww > w {
			// Too long for any line: break it wherever it gets full.
			parts := strings.Split(cond.Wrap(text, w-n), "\n")
			parts = slices.DeleteFunc(parts, func(p string) bool { return p == "" })
			for _, p := range parts[:len(parts)-1] {
				lines = append(lines, line+p)
				line = pad
			}
			last := parts[len(parts)-1]
			line += last + word[len(text):]
			lineWidth = n + cond.StringWidth(last) + spaces
			continue
		}
		line += word
		lineWidth += ww + spaces
	}
	return append(lines, strings.TrimRight(line, " "))
}

// stripANSI removes CSI escape sequences such as colors from s.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// clean makes untrusted text safe to print in a terminal: control
// characters become visible symbols so they can neither break the table
// layout nor inject escape sequences. Newlines are kept for the caller.
func clean(s string) string {
	if !strings.ContainsFunc(s, needsEscape) {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20:
			b.WriteRune(0x2400 + r) // control pictures: ␀ … ␟
		case r == 0x7f:
			b.WriteRune('␡')
		case needsEscape(r):
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func needsEscape(r rune) bool {
	switch {
	case r < 0x20 || r == 0x7f:
		return true
	case r >= 0x80 && r < 0xa0: // C1 controls
		return true
	case r == 0x061c || r == 0x200e || r == 0x200f, // bidi marks
		r >= 0x202a && r <= 0x202e, // bidi embeddings and overrides
		r >= 0x2066 && r <= 0x2069: // bidi isolates
		return true
	}
	return false
}
