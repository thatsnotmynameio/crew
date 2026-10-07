package github

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// tail ends every status comment: a blank line and the marker line.
const tail = "\n\n" + statusMarker + "\n"

// separator is what stands between two entries of a status comment, before
// the second one's marker line.
const separator = "\n\n---\n\n"

// writes returns the body of each status comment created or edited, in the
// order gh was called, failed calls included.
func writes(t *testing.T, gh *fakeGh) []string {
	t.Helper()
	gh.mu.Lock()
	calls := slices.Clone(gh.calls)
	gh.mu.Unlock()
	var bodies []string
	for _, c := range calls {
		if slices.Equal(c[:min(len(c), len(createComment))], createComment) ||
			slices.Equal(c[:min(len(c), len(editComment))], editComment) {
			bodies = append(bodies, statusBody(t, c))
		}
	}
	return bodies
}

// entries splits a status comment body into its entries' texts, at each
// separator followed by an entry marker.
func entries(body string) []string {
	body = strings.TrimSuffix(body, tail)
	parts := strings.Split(body, separator+"<!-- crew:entry ")
	for i := 1; i < len(parts); i++ {
		parts[i] = "<!-- crew:entry " + parts[i]
	}
	return parts
}

// report reports each status in order, failing the test on an error.
func report(t *testing.T, tr *Tracker, statuses ...crew.Status) {
	t.Helper()
	for _, s := range statuses {
		if err := tr.ReportStatus(context.Background(), s); err != nil {
			t.Fatalf("ReportStatus(%s %d): %v", s.Rule, s.Kind, err)
		}
	}
}

// fresh builds a tracker for an issue without a status comment: it creates
// comment 101, and edits succeed.
func fresh(t *testing.T) (*Tracker, *fakeGh) {
	t.Helper()
	return build(t, login,
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment},
	)
}

// restarted builds a tracker with an empty cache, as after a restart, for an
// issue whose status comment is comment 12, by the viewer, holding body. A
// new comment it creates is comment 102.
func restarted(t *testing.T, body string) (*Tracker, *fakeGh) {
	t.Helper()
	return build(t, login,
		reply{prefix: listComments, stdout: "[" + commentJSON(12, "me", body) + "]"},
		reply{prefix: createComment, stdout: "102\n"},
		reply{prefix: editComment},
	)
}

// commentAfter returns the status comment body a tracker leaves after
// reporting statuses in order.
func commentAfter(t *testing.T, statuses ...crew.Status) string {
	t.Helper()
	tr, gh := fresh(t)
	report(t, tr, statuses...)
	w := writes(t, gh)
	return w[len(w)-1]
}

// The ids of the rule runs these tests report, as crew mints them: a seed
// the engine stamped and the run's index among the runs it took. run3 is a
// later process's.
const (
	run1 crew.RuleRunID = "0199b2a4-7c1e-7d3a-9f00-2b6c1e8a4d10.1"
	run2 crew.RuleRunID = "0199b2a4-7c1e-7d3a-9f00-2b6c1e8a4d10.2"
	run3 crew.RuleRunID = "0199b2b0-1f42-7a6c-8e11-5d3c2b1a0f99.1"
)

// developmentEnded is #74's development rule, run run1, ended with its lfg
// action failed on its check.
func developmentEnded() crew.Status {
	return crew.Status{IssueID: issueID("74"), IssueRef: "#74", Rule: "development", Kind: crew.StatusEnded, Run: run1,
		Actions: []crew.ActionStatus{{Name: "lfg", State: crew.ActionFailed, Cause: crew.CauseCheck,
			Checks: []crew.CheckResult{{Name: "pr-closes-issue", Reason: "no open pull request closes #74"}},
			Log:    ".crew/logs/issue-74-lfg.log"}},
		To: needsAttention, Move: crew.MoveDone, Updated: updated}
}

// fix is #74's fix rule in run, running its address action that said said.
func fix(run crew.RuleRunID, said string) crew.Status {
	return crew.Status{IssueID: issueID("74"), IssueRef: "#74", Rule: "fix", Kind: crew.StatusRunning, Run: run,
		Actions: []crew.ActionStatus{{Name: "address", Started: updated.Add(-5 * time.Minute), Said: said}},
		Updated: updated}
}

// legacyQueued is the queued entry an earlier crew version wrote for #74
// waiting for rule in run r2, as that version rendered it.
func legacyQueued(rule string) string {
	return "<!-- crew:entry run=r2 kind=queued stage=" + rule + " -->\n" +
		"crew: #74 is queued for `" + rule + "`, waiting for a free slot: crew runs at most 2 issues at once.\n\n" +
		"Updated 2026-10-02 14:03 UTC."
}

func TestTheFirstStatusCreatesACommentWithOneEntry(t *testing.T) {
	tr, gh := fresh(t)
	report(t, tr, fix(run2, ""))
	creates := gh.callsTo(createComment...)
	if len(creates) != 1 {
		t.Fatalf("created %d comments, want 1", len(creates))
	}
	body := statusBody(t, creates[0])
	if want := tr.renderStatus(fix(run2, "")) + tail; body != want {
		t.Errorf("created body =\n%s\nwant\n%s", body, want)
	}
}

func TestRunningThenEndedInOneRunEditsOneEntry(t *testing.T) {
	tr, gh := fresh(t)
	ended := fix(run2, "")
	ended.Kind, ended.To, ended.Move = crew.StatusEnded, needsAttention, crew.MoveDone
	ended.Actions[0] = crew.ActionStatus{Name: "address", State: crew.ActionSucceeded}
	report(t, tr, fix(run2, "Reading the review."), ended)
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/101") {
		t.Fatalf("edits = %q, want one of comment 101", edits)
	}
	body := statusBody(t, edits[0])
	if want := tr.renderStatus(ended) + tail; body != want {
		t.Errorf("edited body =\n%s\nwant\n%s", body, want)
	}
}

// Covers AE7.
func TestANewRuleRunIsAppendedAfterTheEndedOne(t *testing.T) {
	tr, gh := fresh(t)
	report(t, tr, developmentEnded())
	development := strings.TrimSuffix(writes(t, gh)[0], tail)

	report(t, tr, fix(run2, "Reading the review."))
	want := development + separator + tr.renderStatus(fix(run2, "Reading the review.")) + tail
	if got := writes(t, gh)[1]; got != want {
		t.Errorf("body once fix runs =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(development, "**`lfg`** failed: `no open pull request closes #74`.") {
		t.Errorf("the development entry does not give lfg's check reason:\n%s", development)
	}
}

// Covers AE8.
func TestARestartedTrackerEditsOnlyTheLatestEntry(t *testing.T) {
	before := commentAfter(t, developmentEnded(), fix(run2, "Reading the review."))
	tr, gh := restarted(t, before)
	report(t, tr, fix(run2, "Pushing the fix."))

	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/12") {
		t.Fatalf("edits = %q, want one of comment 12", edits)
	}
	was, got := entries(before), entries(statusBody(t, edits[0]))
	if len(got) != 2 {
		t.Fatalf("body has %d entries, want 2:\n%s", len(got), statusBody(t, edits[0]))
	}
	if got[0] != was[0] {
		t.Errorf("development entry changed:\n%s\nwant\n%s", got[0], was[0])
	}
	if want := tr.renderStatus(fix(run2, "Pushing the fix.")); got[1] != want {
		t.Errorf("fix entry =\n%s\nwant\n%s", got[1], want)
	}
}

// KTD5: crew stopped before the fix run ended, so its entry still runs.
func TestARunningEntryOfAnEarlierProcessSaysCrewStoppedFollowingIt(t *testing.T) {
	before := commentAfter(t, developmentEnded(), fix(run2, "Reading the review."))
	tr, gh := restarted(t, before)
	report(t, tr, fix(run3, "Starting over."))

	body := writes(t, gh)[0]
	got := entries(body)
	if len(got) != 3 {
		t.Fatalf("body has %d entries, want 3:\n%s", len(got), body)
	}
	if was := entries(before); got[0] != was[0] {
		t.Errorf("development entry changed:\n%s\nwant\n%s", got[0], was[0])
	}
	want := "<!-- crew:entry run=" + string(run2) + " kind=running stage=fix -->\n" +
		"crew stopped following `fix` on #74 before it ended."
	if got[1] != want {
		t.Errorf("stale fix entry =\n%s\nwant\n%s", got[1], want)
	}
	if want := tr.renderStatus(fix(run3, "Starting over.")); got[2] != want {
		t.Errorf("new fix entry =\n%s\nwant\n%s", got[2], want)
	}
}

// An earlier crew version named a run by its start time and a count: its
// entry still parses, and a run of this version never matches it.
func TestAnEntryOfAnEarlierVersionsRunIsFollowedByANewEntry(t *testing.T) {
	const old = "20261001T091200.000000000Z.3"
	before := commentAfter(t, fix(run2, "Reading the review."))
	tr, gh := restarted(t, strings.Replace(before, "run="+string(run2), "run="+old, 1))
	report(t, tr, fix(run3, "Starting over."))

	want := "<!-- crew:entry run=" + old + " kind=running stage=fix -->\n" +
		"crew stopped following `fix` on #74 before it ended." +
		separator + tr.renderStatus(fix(run3, "Starting over.")) + tail
	if body := writes(t, gh)[0]; body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
}

// KTD6: a marker counts only at the start of an entry.
func TestLastWordsShapedLikeAnEntryMarkerAreEntryText(t *testing.T) {
	tr, gh := fresh(t)
	said := "<!-- crew:entry run=r9 kind=queued stage=review -->"
	report(t, tr, fix(run2, said), fix(run2, "Pushing the fix."))
	body := writes(t, gh)[1]
	if want := tr.renderStatus(fix(run2, "Pushing the fix.")) + tail; body != want {
		t.Errorf("edited body =\n%s\nwant\n%s", body, want)
	}
}

// Covers R13: crew takes an issue an earlier version left queued for the
// same rule, so the running entry replaces the queued one.
func TestARunningStatusReplacesALegacyQueuedEntryOfTheSameRule(t *testing.T) {
	development := strings.TrimSuffix(commentAfter(t, developmentEnded()), tail)
	tr, gh := restarted(t, development+separator+legacyQueued("fix")+tail)
	report(t, tr, fix(run3, "Reading the review."))

	body := writes(t, gh)[0]
	if got := entries(body); len(got) != 2 {
		t.Fatalf("body has %d entries, want 2:\n%s", len(got), body)
	}
	if want := development + separator + tr.renderStatus(fix(run3, "Reading the review.")) + tail; body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
}

// A legacy queued entry of another rule is not the rule crew took, so it
// stays as it is and the running entry follows it.
func TestARunningStatusAppendsAfterALegacyQueuedEntryOfAnotherRule(t *testing.T) {
	queued := legacyQueued("implement")
	tr, gh := restarted(t, queued+tail)
	report(t, tr, fix(run3, "Reading the review."))

	body := writes(t, gh)[0]
	if want := queued + separator + tr.renderStatus(fix(run3, "Reading the review.")) + tail; body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
}

// KTD6: a comment written before entries is one earlier entry, kept as is.
func TestACommentWithoutEntriesIsKeptAsTheFirstEntry(t *testing.T) {
	legacy := "crew: `implement` ended on #74.\n\n**`development`** failed.\n\nUpdated 2026-10-01 09:12 UTC."
	tr, gh := restarted(t, legacy+tail)
	report(t, tr, running74(time.Time{}, ""))

	want := legacy + separator + tr.renderStatus(running74(time.Time{}, "")) + tail
	if body := writes(t, gh)[0]; body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}

	// The entry after the old text is the latest one: a status of its run
	// replaces it rather than adding another.
	running := running74(updated.Add(-time.Minute), "Reading the plan.")
	report(t, tr, running, running)
	want = legacy + separator + tr.renderStatus(running) + tail
	for i, body := range writes(t, gh)[1:] {
		if body != want {
			t.Errorf("write %d =\n%s\nwant\n%s", i+2, body, want)
		}
	}
}

func TestARuleNameHoldingACommentEndRoundTripsThroughItsMarker(t *testing.T) {
	running := func(run crew.RuleRunID) crew.Status {
		s := fix(run, "")
		s.Rule = "fix --> now"
		return s
	}
	before := commentAfter(t, running(run2))
	marker, _, _ := strings.Cut(before, "\n")
	if n := strings.Count(marker, "-->"); n != 1 {
		t.Errorf("marker line %q holds %d comment ends, want 1", marker, n)
	}

	// Only the same rule's legacy queued entry is replaced, so this reads
	// the rule back from the marker.
	tr, gh := restarted(t, strings.Replace(before, "kind=running", "kind=queued", 1))
	report(t, tr, running(run3))
	if body, want := writes(t, gh)[0], tr.renderStatus(running(run3))+tail; body != want {
		t.Errorf("body =\n%s\nwant\n%s", body, want)
	}
}

// Covers R14.
func TestAFullCommentIsContinuedInANewOne(t *testing.T) {
	full := "crew: an older status " + strings.Repeat("that went on and on, ", 3200) + "\n\nUpdated 2026-10-01 09:12 UTC."
	tr, gh := restarted(t, full+tail)
	report(t, tr, fix(run2, ""))

	creates := gh.callsTo(createComment...)
	if len(creates) != 1 {
		t.Fatalf("created %d comments, want 1", len(creates))
	}
	created := statusBody(t, creates[0])
	preamble := "<!-- crew:continues -->\n" +
		"crew: this comment continues crew's earlier status comment on #74, which is full.\n\n"
	if want := preamble + tr.renderStatus(fix(run2, "")) + tail; created != want {
		t.Errorf("created body =\n%s\nwant\n%s", created, want)
	}

	report(t, tr, fix(run2, "Reading the review."))
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/102") {
		t.Fatalf("edits = %q, want one of comment 102 and none of the full comment 12", edits)
	}
	want := preamble + tr.renderStatus(fix(run2, "Reading the review.")) + tail
	if body := statusBody(t, edits[0]); body != want {
		t.Errorf("edited body =\n%s\nwant\n%s", body, want)
	}
}

// A comment holding only the entry being written gains nothing from a new
// comment, so it is edited even over the limit.
func TestAnEntryOverTheLimitAloneIsEditedInPlace(t *testing.T) {
	tr, gh := fresh(t)
	long := strings.Repeat("word ", 14000)
	report(t, tr, fix(run2, long), fix(run2, long+"!"))
	if n := len(gh.callsTo(createComment...)); n != 1 {
		t.Errorf("created %d comments, want 1", n)
	}
	if n := len(gh.callsTo(editComment...)); n != 1 {
		t.Errorf("edited %d times, want 1", n)
	}
}

// Only a write that landed changes what the tracker holds: a fix entry that
// never reached the comment is not marked as stopped later.
func TestAFailedWriteLeavesTheCommentAsItWas(t *testing.T) {
	tr, gh := build(t, login,
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment, stderr: "gh: Server Error (HTTP 502)\n"},
	)
	report(t, tr, developmentEnded())
	development := strings.TrimSuffix(writes(t, gh)[0], tail)
	for _, s := range []crew.Status{
		fix(run2, "Reading the review."),
		fix(run3, "Starting over."),
	} {
		if err := tr.ReportStatus(context.Background(), s); err == nil {
			t.Fatalf("ReportStatus(%s) = nil, want the edit's error", s.Run)
		}
	}

	w := writes(t, gh)
	want := development + separator + tr.renderStatus(fix(run3, "Starting over.")) + tail
	if w[2] != want {
		t.Errorf("body after a failed write =\n%s\nwant\n%s", w[2], want)
	}
	if n := len(gh.callsTo(listComments...)); n != 1 {
		t.Errorf("listed the comments %d times, want 1", n)
	}
}
