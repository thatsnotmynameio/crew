package app_test

import (
	"context"
	"slices"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// unstamped returns the lines of out with each line's time stamp removed,
// failing the test for a line that has none.
func unstamped(t *testing.T, out string) []string {
	t.Helper()
	if out == "" {
		return nil
	}
	var texts []string
	for line := range strings.SplitSeq(strings.TrimSuffix(out, "\n"), "\n") {
		if !stamped.MatchString(line) {
			t.Errorf("line %q is not a timestamped line", line)
		}
		texts = append(texts, stamped.ReplaceAllString(line, ""))
	}
	return texts
}

// runOneIssue runs crew until the fake harness's first session ends well,
// then stops it with SIGTERM and returns stdout's lines, unstamped.
func runOneIssue(t *testing.T, r *crewRun, h *fake.Harness) []string {
	t.Helper()
	r.start()
	next(t, h).End(success)
	synctest.Wait()
	r.signals <- syscall.SIGTERM
	if code := <-r.code; code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, r.stderr)
	}
	return unstamped(t, r.stdout.String())
}

// Covers AE1 and AE4: each step prints as it starts, one per bot, then
// the bots' warnings, then the event lines.
func TestTheBootLogPrintsEachStepBeforeTheWarningsAndTheEventLines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewPreparingTracker(issue("1", ready))
		tr.ReportStep("checking the gh login")
		h := fake.NewPreparingHarness()
		h.ReportStep("looking for claude on PATH")
		r := options(t, withOps(), tr, h)
		r.opts.Terminal, r.opts.Plain = true, true
		r.opts.Bots = func(ctx context.Context, _ string, names []string) (app.Bots, error) {
			for _, name := range names {
				port.Step(ctx, "making mate "+name+" act")
			}
			return app.Bots{Warnings: []string{"mate ops is not installed on thatsnotmynameio/crew"}}, nil
		}

		got := runOneIssue(t, r, h.Harness)

		want := []string{
			"loading .crew/config.yaml",
			"making mate ops act",
			"checking the gh login",
			"looking for claude on PATH",
			"reading the run journal",
			"warning: mate ops is not installed on thatsnotmynameio/crew",
		}
		if len(got) <= len(want) || !slices.Equal(got[:len(want)], want) {
			t.Fatalf("stdout starts with %q, want %q then the event lines", got, want)
		}
		if first := got[len(want)]; !strings.HasPrefix(first, "implement took #1") {
			t.Errorf("the line after the boot log is %q, want the first event line", first)
		}
	})
}

// Covers AE2: without bots, no bot step runs, so none prints.
func TestWithoutBotsTheBootLogHasNoBotLine(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := fake.NewHarness()
		r := options(t, oneAction, fake.NewTracker(issue("1", ready)), h)
		r.opts.Bots = func(context.Context, string, []string) (app.Bots, error) {
			t.Error("the mates were made to act, with none in the config")
			return app.Bots{}, nil
		}

		got := runOneIssue(t, r, h)

		want := []string{"loading .crew/config.yaml", "reading the run journal"}
		if len(got) <= len(want) || !slices.Equal(got[:len(want)], want) {
			t.Fatalf("stdout starts with %q, want %q then the event lines", got, want)
		}
		if first := got[len(want)]; !strings.HasPrefix(first, "implement took #1") {
			t.Errorf("the line after the boot log is %q, want the first event line", first)
		}
	})
}
