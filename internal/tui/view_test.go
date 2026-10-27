package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestSlice(t *testing.T) {
	for _, tc := range []struct {
		line    string
		from, n int
		want    string
	}{
		{"abcdef", 2, 3, "cde"},
		{"abc", 1, 4, "bc  "},  // padded past the end
		{"abc", 5, 2, "  "},    // all past the end
		{"東京タワー", 1, 4, " 京 "}, // wide characters cut by both edges
		{"東京タワー", 2, 4, "京タ"},
		{"a\x1b[1;34mbcd\x1b[0me", 2, 2, "\x1b[1;34mcd\x1b[0m"}, // color in effect
		{"a\x1b[1;34mbc\x1b[0mde", 2, 3, "\x1b[1;34mc\x1b[0mde"},
		{"ab\x1b[90mc\x1b[0m", 0, 2, "ab"},
	} {
		if got := slice(tc.line, tc.from, tc.n); got != tc.want {
			t.Errorf("slice(%q, %d, %d) = %q, want %q", tc.line, tc.from, tc.n, got, tc.want)
		}
	}
}

func TestWidth(t *testing.T) {
	if got := width("a\x1b[1;34m東京\x1b[0m"); got != 5 {
		t.Errorf("width = %d, want 5", got)
	}
}

// lines returns n lines like "row 1 ………", each w cells wide.
func lines(n, w int) []string {
	out := make([]string, n)
	for i := range out {
		s := fmt.Sprintf("row %d ", i+1)
		out[i] = s + strings.Repeat("x", max(w-len(s), 0))
	}
	return out
}

func TestViewerScroll(t *testing.T) {
	head := []string{"header", "------"}
	v := &viewer{draw: func(int) ([]string, int) {
		return append(slices.Clone(head), lines(100, 50)...), len(head)
	}}
	v.resize(20, 10)
	f := v.frame()
	if len(f) != 10 {
		t.Fatalf("frame has %d rows, want 10", len(f))
	}
	// The header stays, then rows 1-6, the horizontal bar and the status.
	if !strings.HasPrefix(f[0], "header") || !strings.HasPrefix(f[2], "row 1 ") || !strings.HasPrefix(f[7], "row 6 ") {
		t.Errorf("frame =\n%s", strings.Join(f, "\n"))
	}
	if !strings.HasSuffix(f[2], vThumb) || !strings.HasSuffix(f[7], vTrack) {
		t.Errorf("no vertical scroll bar:\n%s", strings.Join(f, "\n"))
	}
	if !strings.HasPrefix(f[8], hThumb) || !strings.Contains(f[9], "line 1-6/100") {
		t.Errorf("no horizontal scroll bar or status:\n%s", strings.Join(f, "\n"))
	}

	v.key("G")
	if f := v.frame(); !strings.HasPrefix(f[0], "header") || !strings.HasPrefix(f[7], "row 100 ") || !strings.HasSuffix(f[7], vThumb) {
		t.Errorf("after G, frame =\n%s", strings.Join(f, "\n"))
	}
	v.key("$")
	if f := v.frame(); !strings.HasSuffix(f[8], hThumb+" ") || v.left != 50-19 {
		t.Errorf("after $, left = %d, frame =\n%s", v.left, strings.Join(f, "\n"))
	}
	v.key("0")
	v.key("g")
	v.key("k") // can't go above the top
	if v.top != 0 || v.left != 0 {
		t.Errorf("after 0 g k, top = %d, left = %d", v.top, v.left)
	}
	if !v.key("q") {
		t.Error("q doesn't quit")
	}
}

func TestViewerFit(t *testing.T) {
	var widths []int
	v := &viewer{draw: func(w int) ([]string, int) {
		widths = append(widths, w)
		if w == 0 {
			w = 200
		}
		return lines(5, w), 0
	}}
	v.resize(40, 10)
	v.key("w")
	v.resize(30, 10)
	v.key("w")
	v.resize(20, 10) // drawn at its natural width, so not redrawn
	if want := []int{0, 39, 29, 0}; !slices.Equal(widths, want) {
		t.Errorf("drawn at widths %v, want %v", widths, want)
	}
	// Everything fits: no scroll bars, and the lines are padded out.
	v.key("w")
	f := v.frame()
	if len(f) != 10 || strings.ContainsAny(strings.Join(f, ""), vThumb+hThumb) || width(f[0]) != 20 {
		t.Errorf("frame =\n%s", strings.Join(f, "\n"))
	}
}

func TestViewerMouse(t *testing.T) {
	// 100 lines of 50 cells on a 20x10 screen: rows 0-7 show lines, the
	// vertical bar is at x = 19, the horizontal one at y = 8.
	v := &viewer{draw: func(int) ([]string, int) { return lines(100, 50), 0 }}
	v.resize(20, 10)

	v.scroll(wheelLines, 0)
	if v.top != 3 {
		t.Errorf("after the wheel, top = %d, want 3", v.top)
	}
	v.scroll(0, -wheelCells) // can't go left of the start
	if v.left != 0 {
		t.Errorf("after the wheel left, left = %d, want 0", v.left)
	}

	// Clicking the bottom of the vertical track jumps to the end, and
	// dragging the thumb back up to the top scrolls back.
	v.press(19, 7)
	if v.top != 100-8 {
		t.Errorf("after a click at the bottom of the bar, top = %d, want %d", v.top, 100-8)
	}
	v.drag(5, 0) // only the row matters
	v.release()
	if v.top != 0 {
		t.Errorf("after dragging the thumb to the top, top = %d, want 0", v.top)
	}
	v.drag(19, 7) // released, so this does nothing
	if v.top != 0 {
		t.Errorf("after moving with the button up, top = %d, want 0", v.top)
	}

	// The horizontal bar works the same way across.
	v.press(18, 8)
	v.release()
	if v.left != 50-19 {
		t.Errorf("after a click at the end of the bar, left = %d, want %d", v.left, 50-19)
	}
	v.press(5, 3) // on the content: nothing happens
	v.drag(0, 0)
	if v.top != 0 || v.left != 50-19 {
		t.Errorf("after pressing on the content, top = %d, left = %d", v.top, v.left)
	}
}

func TestThumb(t *testing.T) {
	// unthumb undoes thumb for every position.
	for _, total := range []int{11, 37, 100, 1000} {
		for first := 0; first <= total-10; first++ {
			pos, _ := thumb(10, 10, total, first)
			back := unthumb(10, 10, total, pos)
			if again, _ := thumb(10, 10, total, back); again != pos {
				t.Errorf("total %d: first %d -> pos %d -> first %d -> pos %d", total, first, pos, back, again)
			}
		}
	}
}

func TestViewerColor(t *testing.T) {
	v := &viewer{draw: func(int) ([]string, int) { return lines(100, 50), 0 }, color: true}
	v.resize(20, 10)
	f := v.frame()
	// Both bars are as thin, and their thumbs are colored apart from the
	// rails and the status line.
	thumb := "\x1b[" + thumbColor + "m"
	if !strings.HasSuffix(f[0], thumb+vThumb+"\x1b[0m") || !strings.HasPrefix(f[8], thumb+hThumb) {
		t.Errorf("frame =\n%q", f)
	}
	if !strings.Contains(f[8], "\x1b["+trackColor+"m"+hTrack) || width(f[8]) != 20 {
		t.Errorf("horizontal bar = %q", f[8])
	}
}

func TestViewerText(t *testing.T) {
	v := &viewer{draw: func(int) ([]string, int) {
		return []string{"\x1b[1;34mkey\x1b[0m │ 東京  ", "a"}, 0
	}}
	v.resize(20, 10)
	if got, want := v.text(), "key │ 東京\na\n"; got != want {
		t.Errorf("text = %q, want %q", got, want)
	}
	v.notice = "copied 2 lines"
	if f := v.frame(); !strings.Contains(f[len(f)-1], "copied 2 lines") || strings.Contains(f[len(f)-1], "q: quit") {
		t.Errorf("status = %q", f[len(f)-1])
	}
}
