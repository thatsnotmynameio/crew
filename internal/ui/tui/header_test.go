package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Covers R3: crew's name, the gradient run, then the repository, time up,
// time left, spend and tokens, and the key for help.
func TestTheHeaderShowsTheRunsDetails(t *testing.T) {
	got := firstLine(fitted(t, 120, 0, handledSnapshot()))

	if !strings.HasPrefix(got, "crew ╱╱╱") {
		t.Errorf("header = %q, want crew's name and a run of ╱", got)
	}
	if !strings.HasSuffix(got, "╱ crew • up 12m • 48m left • $19.86 • 24.1M tokens  ? help") {
		t.Errorf("header = %q, want the details and the key for help", got)
	}
	if n := len([]rune(got)); n != 120 {
		t.Errorf("header is %d columns, want the window's 120", n)
	}
}

func TestANarrowHeaderDropsTokensThenSpendBeforeTheRepository(t *testing.T) {
	got := firstLine(fitted(t, 60, 0, handledSnapshot()))
	if strings.Contains(got, "tokens") || !strings.Contains(got, "$19.86") {
		t.Errorf("header at 60 = %q, want the tokens dropped and the spend kept", got)
	}

	got = firstLine(fitted(t, 45, 0, handledSnapshot()))
	if strings.Contains(got, "$") || !strings.Contains(got, "crew • up 12m") {
		t.Errorf("header at 45 = %q, want the spend dropped and the repository kept", got)
	}
}

func TestWithoutARunTimeLimitTheHeaderShowsNoTimeLeft(t *testing.T) {
	u := runningSnapshot()
	u.Snapshot.RunTimeLimit = 0

	got := firstLine(fitted(t, 80, 0, u))

	if !strings.Contains(got, "up 12m") || strings.Contains(got, "left") {
		t.Errorf("header = %q, want the time up and no time left", got)
	}
}

func TestBeforeTheFirstPollTheHeaderShowsNoUptime(t *testing.T) {
	h := newHarness(t, 80)

	if got := firstLine(h.view()); strings.Contains(got, "up ") {
		t.Errorf("header = %q, want no uptime", got)
	}
}

func TestATickAMinuteLaterAdvancesTheUptimeAndLowersTheTimeLeft(t *testing.T) {
	h := newHarness(t, 80)
	h.send(updateMsg(runningSnapshot()))

	h.clock = h.clock.Add(time.Minute)
	h.send(tickMsg{})

	contains(t, firstLine(h.view()), "up 13m • 47m left")
}

// Covers KTD16.
func TestAfterTheRunTimeIsUpTheHeaderSaysCrewIsWindingDown(t *testing.T) {
	h := newHarness(t, 80)

	h.send(updateMsg(windingDownSnapshot()))

	got := firstLine(h.view())
	if !strings.HasSuffix(got, " WINDING DOWN ") || strings.Contains(got, "left") {
		t.Errorf("header = %q, want the WINDING DOWN pill and no time left", got)
	}
	golden(t, "winding-down", h.view())
}

// Covers R7 and R29: the header reads on a light background too.
func TestALightBackgroundRestylesTheView(t *testing.T) {
	h := newHarness(t, 80)
	dark := h.raw()

	h.send(tea.BackgroundColorMsg{Color: lipgloss.Color("#ffffff")})

	if h.raw() == dark {
		t.Error("the view kept its dark styles on a light background")
	}
	contains(t, h.raw(), "\x1b[1;38;2;22;21;29m") // the light title role, #16151d, in bold
}
