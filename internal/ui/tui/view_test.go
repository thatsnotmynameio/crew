package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
)

// durations pairs each duration with how elapsed and short format it.
var durations = []struct {
	d              time.Duration
	elapsed, short string
}{
	{-time.Second, "0m00s", "0s"},
	{0, "0m00s", "0s"},
	{999 * time.Millisecond, "0m00s", "0s"},
	{30 * time.Second, "0m30s", "30s"},
	{59*time.Second + 999*time.Millisecond, "0m59s", "59s"},
	{time.Minute, "1m00s", "1m"},
	{5*time.Minute + 3*time.Second, "5m03s", "5m"},
	{time.Hour - time.Millisecond, "59m59s", "59m"},
	{time.Hour, "1h00m00s", "1h00m"},
	{time.Hour + 5*time.Minute + 3*time.Second, "1h05m03s", "1h05m"},
	{26*time.Hour + 59*time.Minute + 59*time.Second, "26h59m59s", "26h59m"},
}

func TestDurationsFormatInWholeSecondsMinutesAndHours(t *testing.T) {
	for _, tt := range durations {
		if got := elapsed(tt.d); got != tt.elapsed {
			t.Errorf("elapsed(%v) = %q, want %q", tt.d, got, tt.elapsed)
		}
		if got := short(tt.d); got != tt.short {
			t.Errorf("short(%v) = %q, want %q", tt.d, got, tt.short)
		}
	}
}

// A written board (KTD9): #20 held and running in bugs, #21 unheld in
// ideas and bugs, #22 in done, while the last board read failed.
func TestAWrittenBoardRendersTheGoldenView(t *testing.T) {
	h := newBoardHarness(t, 80, crewNotify, ideasBugsDone)
	u := onBoard(held(twenty, "fix", "lfg", core.ClaimRunning),
		labeled(twentyOne, "crew:brainstorm:ready", "bug"), labeled(twenty, "bug"),
		labeled(twentyTwo, "crew:triage:done"))
	u.Snapshot.BoardFailure = "gh: rate limited"

	h.send(updateMsg(u))

	golden(t, "written-board", h.view())
}

// oldWords are the words crew's interface no longer says (R17).
var oldWords = regexp.MustCompile(`(?i)\b(boss(es)?|mates?|stages?|workflows?)\b`)

// Covers R17: no golden view says boss, mate, stage or workflow.
func TestNoGoldenViewSaysAnOldWord(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.golden"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("golden views: %v, %v", paths, err)
	}
	for _, path := range paths {
		view, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if found := oldWords.FindAllString(string(view), -1); len(found) > 0 {
			t.Errorf("%s says %q", path, found)
		}
	}
}
