package tui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Show shows the output draw draws in a full-screen viewer on the terminal
// until the user quits. The output starts drawn to fit the screen width if
// fit is set, or else at its natural width, scrolling sideways; the w key
// switches between the two. The mouse wheel scrolls, sideways with shift
// held, and the scroll bars can be clicked and dragged, in color if color
// is set. The c key copies the output as drawn. Keys are read from the
// terminal itself when standard input is not one, so it may be a pipe.
func Show(draw Draw, fit, color bool) error {
	_, err := tea.NewProgram(model{&viewer{draw: draw, fit: fit, color: color}}).Run()
	if errors.Is(err, tea.ErrInterrupted) {
		return nil
	}
	return err
}

// model runs the viewer as a Bubble Tea program.
type model struct {
	v *viewer
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	v := m.v
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		v.resize(size.Width, size.Height)
		return m, nil
	}
	if v.lines == nil {
		return m, nil // nothing is drawn until the screen size is known
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if msg.String() == "c" {
			return m, copyText(v.text())
		}
		if v.key(msg.String()) {
			return m, tea.Quit
		}
	case copiedMsg:
		return m, m.notify(msg.notice())
	case clearMsg:
		if v.notice == msg.notice {
			v.notice = ""
		}
	case tea.MouseWheelMsg:
		mouse := msg.Mouse()
		shift := mouse.Mod.Contains(tea.ModShift)
		switch {
		case mouse.Button == tea.MouseWheelLeft,
			mouse.Button == tea.MouseWheelUp && shift:
			v.scroll(0, -wheelCells)
		case mouse.Button == tea.MouseWheelRight,
			mouse.Button == tea.MouseWheelDown && shift:
			v.scroll(0, wheelCells)
		case mouse.Button == tea.MouseWheelUp:
			v.scroll(-wheelLines, 0)
		case mouse.Button == tea.MouseWheelDown:
			v.scroll(wheelLines, 0)
		}
	case tea.MouseClickMsg:
		if mouse := msg.Mouse(); mouse.Button == tea.MouseLeft {
			v.press(mouse.X, mouse.Y)
		}
	case tea.MouseMotionMsg:
		mouse := msg.Mouse()
		v.drag(mouse.X, mouse.Y)
	case tea.MouseReleaseMsg:
		v.release()
	}
	return m, nil
}

func (m model) View() tea.View {
	var content string
	if m.v.lines != nil {
		content = strings.Join(m.v.frame(), "\n")
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	return view
}

// noticeTime is how long a notice stays in the status line.
const noticeTime = 3 * time.Second

// clearMsg asks for the notice to be cleared, unless another has replaced
// it since.
type clearMsg struct{ notice string }

// notify shows notice in the status line for a while.
func (m model) notify(notice string) tea.Cmd {
	m.v.notice = notice
	return tea.Tick(noticeTime, func(time.Time) tea.Msg { return clearMsg{notice} })
}

// copiedMsg reports the outcome of copying text to the clipboard.
type copiedMsg struct {
	lines int
	how   string // the command used, or "" for the terminal
	err   error
}

func (c copiedMsg) notice() string {
	switch {
	case c.err != nil:
		return "copy failed: " + c.err.Error()
	case c.how == "":
		return fmt.Sprintf("sent %d lines to the terminal clipboard", c.lines)
	}
	return fmt.Sprintf("copied %d lines", c.lines)
}

// copyText returns a command that puts s on the system clipboard, with a
// clipboard command of the system if it has one and through the terminal
// (OSC 52) otherwise, as when it runs over SSH.
func copyText(s string) tea.Cmd {
	lines := strings.Count(s, "\n")
	return func() tea.Msg {
		how, err := clipboardCommand(s)
		if errors.Is(err, errNoClipboard) {
			// Set the clipboard through the terminal, then report it.
			return tea.Sequence(tea.SetClipboard(s), func() tea.Msg {
				return copiedMsg{lines: lines}
			})()
		}
		return copiedMsg{lines: lines, how: how, err: err}
	}
}
