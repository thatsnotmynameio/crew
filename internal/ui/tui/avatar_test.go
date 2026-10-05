package tui

import (
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/thatsnotmynameio/crew/internal/core"
)

// plain is rows with their styles stripped.
func plain(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Strip(r)
	}
	return out
}

// Covers R2: an avatar is three rows of five cells, the same each time for
// a seed, and reads the same from either side.
func TestAnAvatarIsASymmetricPatternOfItsSeed(t *testing.T) {
	s := newStyles(true)
	for _, seed := range []string{"clerk", "developer", "reviewer", "octocat", "you"} {
		rows := plain(s.avatar(seed, s.avatars[0]))
		if len(rows) != avatarRows {
			t.Fatalf("%s's avatar has %d rows, want %d", seed, len(rows), avatarRows)
		}
		for _, r := range rows {
			cells := []rune(r)
			if len(cells) != avatarWidth {
				t.Errorf("%s's avatar row %q is %d cells, want %d", seed, r, len(cells), avatarWidth)
			}
			if mirrored := slices.Clone(cells); !slices.Equal(cells, reversed(mirrored)) {
				t.Errorf("%s's avatar row %q is not symmetric", seed, r)
			}
		}
		if again := plain(s.avatar(seed, s.avatars[0])); !slices.Equal(rows, again) {
			t.Errorf("%s's avatar changed between draws: %q then %q", seed, rows, again)
		}
	}
	if a, b := plain(s.avatar("clerk", s.offline)), plain(s.avatar("developer", s.offline)); slices.Equal(a, b) {
		t.Errorf("clerk and developer share the avatar %q", a)
	}
}

// reversed returns r in reverse order.
func reversed(r []rune) []rune {
	slices.Reverse(r)
	return r
}

// Covers R2 and R13: an avatar takes one of its palette's hues, never the
// warning or error colour, and a bot that is not acting goes grey; you
// keep your hue.
func TestAnAvatarTakesAnAccentHueOrGoesGreyOffline(t *testing.T) {
	for _, dark := range []bool{true, false} {
		s, p := newStyles(dark), lightPalette()
		if dark {
			p = darkPalette()
		}
		if slices.Contains(s.avatars[:], p.warning) || slices.Contains(s.avatars[:], p.error) {
			t.Errorf("dark=%v: an avatar hue is the warning or error colour", dark)
		}
		huesAndGrey(t, s)
	}
}

// huesAndGrey checks s gives acting bots and you a hue, and bots that are
// not acting the offline grey.
func huesAndGrey(t *testing.T, s styles) {
	t.Helper()
	for _, e := range []core.BotView{
		{Name: "clerk", Acting: true, State: "acting"},
		{Name: "you", You: true, Login: "octocat"},
		{Name: "you", You: true},
	} {
		if c := s.avatarColour(e); !slices.Contains(s.avatars[:], c) {
			t.Errorf("%s's avatar colour is not one of the hues", e.Name)
		}
	}
	for _, state := range []string{"cannot act: no key", "writes as you", "token not renewed"} {
		if c := s.avatarColour(core.BotView{Name: "clerk", State: state}); c != s.offline {
			t.Errorf("a bot whose state is %q is not grey", state)
		}
	}
}

// Covers KTD4: your avatar is seeded with your login, so each boss gets
// their own, and with "you" while the login is unknown.
func TestYourAvatarIsSeededWithYourLogin(t *testing.T) {
	if got := avatarSeed(core.BotView{Name: "you", You: true, Login: "octo\x1b[31mcat"}); got != "octocat" {
		t.Errorf("seed is %q, want the clean login octocat", got)
	}
	if got := avatarSeed(core.BotView{Name: "you", You: true}); got != "you" {
		t.Errorf("seed is %q, want you", got)
	}
	if got := avatarSeed(core.BotView{Name: "clerk", Login: "octocat"}); got != "clerk" {
		t.Errorf("seed is %q, want the bot's name", got)
	}
}

// Covers R2: the avatar's rows are drawn in its colour.
func TestAnAvatarIsDrawnInItsColour(t *testing.T) {
	s := newStyles(true)
	want := lipgloss.NewStyle().Foreground(s.avatars[2]).Render("x")
	prefix, _, _ := strings.Cut(want, "x")
	for _, r := range s.avatar("developer", s.avatars[2]) {
		if !strings.HasPrefix(r, prefix) {
			t.Errorf("avatar row %q is not drawn in its colour %q", r, prefix)
		}
	}
}
