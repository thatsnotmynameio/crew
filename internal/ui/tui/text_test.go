package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Covers KTD14.
func TestCleanStripsEscapesAndControlCharactersOntoOneLine(t *testing.T) {
	for in, want := range map[string]string{
		"Rule\x1b[31m labels\x1b[0m": "Rule labels",
		"ring\x07 the bell":          "ring the bell",
		"title\x1b]0;evil\x07 after": "title after",
		"two\nlines\r\n  and\ttabs":  "two lines and tabs",
		"c1 \u009b31m control":       "c1 31m control",
		"plain":                      "plain",
		"  spaced   out  ":           "spaced out",
	} {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}

// Covers R23: the screen shows a session's or a check's text built by crew's
// text types exactly as it showed the raw text. Invalid UTF-8 is left out:
// ansi.Strip keeps a broken rune's bytes, which clean shows as U+FFFD, where
// the text types drop them.
func TestCleanShowsBuiltTextAsItShowsTheRawText(t *testing.T) {
	hostile := []string{
		"the check tests passed: ok",
		"a\x00b", "a\x7fb", "a\u0085b", "fatal: x\nhint: y", "a\rb", "a\vb", "a\fb", "a\tb",
		"\x1b[31mred\x1b[0m", "\x1b[38;2;1;2;3mrgb", "\x1b[?25lhidden",
		"a\x1b]0;evil\x07b", "a\x1b]0;evil\x1b\\b", "a\x1b]0;evil and the rest",
		"\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\",
		"\x1b(Bdone\x1b[m", "a\x1b7b", "a\x1b%Gb", "a\x1b\x1b[1mb", "a\x1b",
		"a\x1bPq#0;2;0;0;0\x1b\\b", "a\x1bP\x1b\\b", "a\x1b_Gf=24;payload\x1b\\b", "a\x1b^pm\x1b\\b", "a\x1bXsos\x1b\\b",
		"a\x9b31mb", "c1 \u009b31m control",
		"a\x1b[3\n1mb", "a\x1b[3\x18b", "a\x1b[3\x7f1mb", "a\x1b[ 1mb", "a\x1b\nAb",
		"a\x1b]0;x\x1ab", "a\x1b_é\x1b\\b", "a\x1b]0;Ü\x07b",
	}
	for _, raw := range hostile {
		want := clean(raw)
		for typ, built := range map[string]string{
			"SessionText": crew.NewSessionText(raw).String(),
			"Said":        crew.NewSaid(raw).String(),
			"CheckReason": crew.NewCheckReason(raw).String(),
		} {
			if got := clean(built); got != want {
				t.Errorf("clean of %s built from %q = %q, want %q as from the raw text", typ, raw, got, want)
			}
		}
	}
}

func TestCappedCutsOutsideTextTo200Runes(t *testing.T) {
	got := capped(strings.Repeat("é", 300))

	if n := len([]rune(got)); n != maxOutside || !strings.HasSuffix(got, ellipsis) {
		t.Errorf("capped is %d runes ending %q, want %d ending in an ellipsis", n, got[len(got)-3:], maxOutside)
	}
}

// Covers R6 and R28: a reference links to its page, underlined.
func TestAReferenceLinksToItsPage(t *testing.T) {
	s := newStyles(true)

	got := s.link("#12", "https://github.com/o/r/issues/12")

	if !strings.Contains(got, "\x1b]8;;https://github.com/o/r/issues/12") {
		t.Errorf("link = %q, want an OSC 8 hyperlink to the issue", got)
	}
	if ansi.Strip(got) != "#12" {
		t.Errorf("link strips to %q, want #12", ansi.Strip(got))
	}
	if plain := s.link("#12", ""); strings.Contains(plain, "\x1b]8;") {
		t.Errorf("a reference without a URL links: %q", plain)
	}
}

// Covers R4: a rule fills the width exactly.
func TestARuleFillsTheWidth(t *testing.T) {
	m := testModel()
	for _, width := range []int{80, 120} {
		r := m.rule("Board", "2 issues", width, false)
		if w := lipgloss.Width(r); w != width {
			t.Errorf("rule at %d is %d wide: %q", width, w, ansi.Strip(r))
		}
		if !strings.HasPrefix(ansi.Strip(r), "Board ─") || !strings.HasSuffix(ansi.Strip(r), "─ 2 issues") {
			t.Errorf("rule = %q", ansi.Strip(r))
		}
	}
	if r := ansi.Strip(m.rule("Events", "a summary far too long for it", 20, true)); r != "▸ Events ───────────" {
		t.Errorf("narrow focused rule = %q, want the summary dropped", r)
	}
}

// testModel is a model of the test snapshots' rules, before any update.
func testModel() Model {
	return New(Config{Now: func() time.Time { return start }, Location: zone, Notify: testNotify})
}
