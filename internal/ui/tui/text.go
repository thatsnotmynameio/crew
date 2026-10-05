package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ellipsis ends a line cut to the window's width.
const ellipsis = "…"

// maxOutside caps the text crew writes into the window title and a
// notification (KTD14).
const maxOutside = 200

// clean returns s as one line of plain text: escape sequences and control
// characters removed, and its words joined by single spaces. Titles, failure
// reasons, session words and event text come from outside crew, and an
// escape sequence in them would reach the terminal (KTD14).
func clean(s string) string {
	s = ansi.Strip(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// capped returns s cleaned and cut to maxOutside runes, for the window title
// and notifications (KTD14).
func capped(s string) string {
	r := []rune(clean(s))
	if len(r) <= maxOutside {
		return string(r)
	}
	return string(r[:maxOutside-1]) + ellipsis
}

// fit cuts s, which may be styled, to width cells, ending a cut line with an
// ellipsis.
func fit(s string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(s, width, ellipsis)
}

// pad cuts or pads s, which may be styled, to exactly width cells.
func pad(s string, width int) string {
	s = fit(s, width)
	if gap := width - lipgloss.Width(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// widest returns the width of the widest of strs, in cells.
func widest(strs []string) int {
	w := 0
	for _, s := range strs {
		w = max(w, lipgloss.Width(s))
	}
	return w
}

// elapsed formats d as 5m03s, or 1h05m03s past an hour.
func elapsed(d time.Duration) string {
	d = max(d, 0)
	minutes, seconds := int(d%time.Hour/time.Minute), int(d%time.Minute/time.Second)
	if d >= time.Hour {
		return fmt.Sprintf("%dh%02dm%02ds", int(d/time.Hour), minutes, seconds)
	}
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

// short formats d as 3h12m, 48m, or 30s under a minute.
func short(d time.Duration) string {
	d = max(d, 0)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}
