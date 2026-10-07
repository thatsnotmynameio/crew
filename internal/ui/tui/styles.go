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
	// avatars are the hues an avatar takes, from the accent family; offline
	// is the grey of a bot that is not acting (KTD5).
	avatars [avatarHues]color.Color
	offline color.Color
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
		avatars: [avatarHues]color.Color{
			lipgloss.Color("#b48cff"), lipgloss.Color("#8f7bff"), lipgloss.Color("#ff60ff"),
			lipgloss.Color("#68ffd6"), lipgloss.Color("#5fd7ff"), lipgloss.Color("#ff9fd2"),
		},
		offline: lipgloss.Color("#4a4760"),
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
		avatars: [avatarHues]color.Color{
			lipgloss.Color("#7a4fd6"), lipgloss.Color("#5a3fd0"), lipgloss.Color("#b02fb0"),
			lipgloss.Color("#0a8f6a"), lipgloss.Color("#1a7fb0"), lipgloss.Color("#c0407f"),
		},
		offline: lipgloss.Color("#6b6785"),
	}
}

// styles are the Lip Gloss styles the view draws with, one per role.
type styles struct {
	// gradientFrom and gradientTo are the header run's stops (R3).
	gradientFrom, gradientTo color.Color
	// avatars and offline are the avatars' colours (KTD5).
	avatars [avatarHues]color.Color
	offline color.Color

	text, title, muted, subtle lipgloss.Style
	accent, strongAccent       lipgloss.Style
	// highlight is the highlighted card's border: the title colour, the
	// brightest on its background (KTD11 of #151).
	highlight                    lipgloss.Style
	success, warning, error      lipgloss.Style
	chip, blockedChip, ref       lipgloss.Style
	warningPill                  lipgloss.Style
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
	chip := lipgloss.NewStyle().Foreground(p.chipText).Background(p.chipBack).Padding(0, 1)
	return styles{
		gradientFrom: p.gradientFrom, gradientTo: p.gradientTo,
		avatars: p.avatars, offline: p.offline,
		text: fg(p.text), title: fg(p.title).Bold(true), muted: fg(p.muted), subtle: fg(p.subtle),
		accent: fg(p.accent), strongAccent: fg(p.strongAccent), highlight: fg(p.title),
		success: fg(p.success), warning: fg(p.warning), error: fg(p.error),
		chip: chip, blockedChip: chip.Foreground(p.warning),
		ref: fg(p.text).Underline(true).UnderlineColor(p.muted),

		warningPill: pill(p.warning),
		helpBox:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.subtle).Padding(0, 1),
		helpKey:     fg(p.accent).Bold(true), helpAction: fg(p.text),
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
