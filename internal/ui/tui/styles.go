package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// palette holds one colour per role (R27), for a dark or a light terminal
// background (R29, KTD12).
type palette struct {
	text, title, muted, subtle  color.Color
	accent, strongAccent        color.Color
	gradientFrom, gradientTo    color.Color
	success, warning, error     color.Color
	chipText, chipBack, pillInk color.Color
}

// darkPalette is the sketch's palette, for a dark background (R27, R28).
func darkPalette() palette {
	return palette{
		text: lipgloss.Color("#d8d6e3"), title: lipgloss.Color("#f1effa"),
		muted: lipgloss.Color("#7d7996"), subtle: lipgloss.Color("#4a4760"),
		accent: lipgloss.Color("#b48cff"), strongAccent: lipgloss.Color("#6b50ff"),
		gradientFrom: lipgloss.Color("#6b50ff"), gradientTo: lipgloss.Color("#ff60ff"),
		success: lipgloss.Color("#68ffd6"), warning: lipgloss.Color("#ffd36b"), error: lipgloss.Color("#ff6b8b"),
		chipText: lipgloss.Color("#d8d6e3"), chipBack: lipgloss.Color("#3a3850"), pillInk: lipgloss.Color("#201f2a"),
	}
}

// lightPalette keeps each role's meaning with shades that read on a light
// background (R29, KTD12).
func lightPalette() palette {
	return palette{
		text: lipgloss.Color("#2b2938"), title: lipgloss.Color("#16151d"),
		muted: lipgloss.Color("#6b6785"), subtle: lipgloss.Color("#c9c6d8"),
		accent: lipgloss.Color("#7a4fd6"), strongAccent: lipgloss.Color("#6b50ff"),
		gradientFrom: lipgloss.Color("#6b50ff"), gradientTo: lipgloss.Color("#ff60ff"),
		success: lipgloss.Color("#0a8f6a"), warning: lipgloss.Color("#9a6a00"), error: lipgloss.Color("#c8264d"),
		chipText: lipgloss.Color("#2b2938"), chipBack: lipgloss.Color("#e4e1ef"), pillInk: lipgloss.Color("#ffffff"),
	}
}

// styles are the Lip Gloss styles the view draws with, one per role.
type styles struct {
	palette

	text, title, muted, subtle   lipgloss.Style
	accent, strongAccent         lipgloss.Style
	success, warning, error      lipgloss.Style
	chip, ref                    lipgloss.Style
	successPill, warningPill     lipgloss.Style
	errorPill                    lipgloss.Style
	helpBox, helpKey, helpAction lipgloss.Style
}

// newStyles returns the styles for a dark background, or a light one.
func newStyles(dark bool) styles {
	p := lightPalette()
	if dark {
		p = darkPalette()
	}
	fg := func(c color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	pill := func(c color.Color) lipgloss.Style {
		return lipgloss.NewStyle().Background(c).Foreground(p.pillInk).Bold(true).Padding(0, 1)
	}
	return styles{
		palette: p,
		text:    fg(p.text), title: fg(p.title).Bold(true), muted: fg(p.muted), subtle: fg(p.subtle),
		accent: fg(p.accent), strongAccent: fg(p.strongAccent),
		success: fg(p.success), warning: fg(p.warning), error: fg(p.error),
		chip: lipgloss.NewStyle().Foreground(p.chipText).Background(p.chipBack).Padding(0, 1),
		ref:  fg(p.text).Underline(true).UnderlineColor(p.muted),

		successPill: pill(p.success), warningPill: pill(p.warning), errorPill: pill(p.error),
		helpBox: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.subtle).Padding(0, 1),
		helpKey: fg(p.accent).Bold(true), helpAction: fg(p.text),
	}
}

// link renders an issue or pull request reference, underlined in the muted
// colour and linked to url when there is one (R6, R28).
func (s styles) link(ref, url string) string {
	st := s.ref
	if url != "" {
		st = st.Hyperlink(url)
	}
	return st.Render(ref)
}

// gradient renders n copies of glyph, coloured from the gradient's first
// stop to its last (R3).
func (s styles) gradient(glyph string, n int) string {
	if n <= 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range lipgloss.Blend1D(n, s.gradientFrom, s.gradientTo) {
		b.WriteString(lipgloss.NewStyle().Foreground(c).Render(glyph))
	}
	return b.String()
}
