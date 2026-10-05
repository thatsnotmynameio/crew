package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/thatsnotmynameio/crew/internal/core"
)

// Covers KTD14.
func TestCleanStripsEscapesAndControlCharactersOntoOneLine(t *testing.T) {
	for in, want := range map[string]string{
		"Stage\x1b[31m labels\x1b[0m": "Stage labels",
		"ring\x07 the bell":           "ring the bell",
		"title\x1b]0;evil\x07 after":  "title after",
		"two\nlines\r\n  and\ttabs":   "two lines and tabs",
		"c1 \u009b31m control":        "c1 31m control",
		"plain":                       "plain",
		"  spaced   out  ":            "spaced out",
	} {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCappedCutsOutsideTextTo200Runes(t *testing.T) {
	got := capped(strings.Repeat("é", 300))

	if n := len([]rune(got)); n != maxOutside || !strings.HasSuffix(got, ellipsis) {
		t.Errorf("capped is %d runes ending %q, want %d ending in an ellipsis", n, got[len(got)-3:], maxOutside)
	}
}

func TestFitLeftKeepsTheEndOfALabel(t *testing.T) {
	if got, want := fitLeft("crew:development:waiting review", 15), "…waiting review"; got != want {
		t.Errorf("fitLeft = %q, want %q", got, want)
	}
	if got := fitLeft("short", 15); got != "short" {
		t.Errorf("fitLeft of a short label = %q", got)
	}
	if got := fitLeft("any", 0); got != "" {
		t.Errorf("fitLeft to no width = %q", got)
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
		r := m.rule("Workflow", "3 in play · 1 waiting", width, false)
		if w := lipgloss.Width(r); w != width {
			t.Errorf("rule at %d is %d wide: %q", width, w, ansi.Strip(r))
		}
		if !strings.HasPrefix(ansi.Strip(r), "Workflow ─") || !strings.HasSuffix(ansi.Strip(r), "─ 3 in play · 1 waiting") {
			t.Errorf("rule = %q", ansi.Strip(r))
		}
	}
	if r := ansi.Strip(m.rule("Events", "a summary far too long for it", 20, true)); r != "▸ Events ───────────" {
		t.Errorf("narrow focused rule = %q, want the summary dropped", r)
	}
}

// Covers R5 and R28: each pill takes its role's colour as background, with
// bold dark text.
func TestEachPillTakesItsRolesColour(t *testing.T) {
	m := testModel()
	ink := "38;2;32;31;42" // #201f2a
	for e, back := range map[*core.HandledView]string{
		new(failedEntry("5", "x", 2, 1, "a", "r")):                         "48;2;255;107;139", // error #ff6b8b
		new(givenUpEntry(entry("6", "x", "s", "ready", 2, 1), "closed")):   "48;2;255;211;107", // warning #ffd36b
		new(entry("7", "x", "s", "crew:development:waiting review", 2, 1)): "48;2;104;255;214", // success #68ffd6
	} {
		got := m.pill(*e)
		if !strings.Contains(got, back) || !strings.Contains(got, ink) || !strings.Contains(got, "1;") {
			t.Errorf("pill %q lacks its background %s, bold or ink %s", got, back, ink)
		}
	}
	if got := ansi.Strip(m.pill(entry("7", "x", "s", "done", 2, 1))); got != " DONE " {
		t.Errorf("pill of a state without colons = %q", got)
	}
}

// testModel is a model of testRules, before any update.
func testModel() Model {
	return New(Config{Now: func() time.Time { return start }, Location: zone, Rules: testRules})
}
