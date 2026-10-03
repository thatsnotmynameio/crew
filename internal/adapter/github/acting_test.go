package github

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Who a gh call runs as: the boss, with no environment of its own, or a
// mate, with its gh config directory.
const (
	asBoss = "boss"
	asMate = "mate"
)

// runsAs returns who c runs as: asMate when it carries an environment of
// its own, asBoss otherwise.
func runsAs(c proc.Command) string {
	if len(c.Env) > 0 || len(c.Unset) > 0 {
		return asMate
	}
	return asBoss
}

// commandsTo returns the recorded commands whose arguments start with
// prefix.
func (f *fakeGh) commandsTo(prefix ...string) []proc.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []proc.Command
	for _, c := range f.cmds {
		if len(c.Args) >= len(prefix) && slices.Equal(c.Args[:len(prefix)], prefix) {
			out = append(out, c)
		}
	}
	return out
}

// The mate ops, crew's writer in the tests below.
const opsLogin = "crew-ops[bot]"

// opsWriter returns ops as the writer, whose Renew counts its calls in
// renewed and fails when renewErr is set.
func opsWriter(renewed *atomic.Int32, renewErr error) port.Identity {
	return port.Identity{
		Mate: "ops", Login: opsLogin,
		Env:   []string{"GH_CONFIG_DIR=/run/crew-mates/ops/crew"},
		Unset: []string{"GH_TOKEN", "GITHUB_TOKEN"},
		Renew: func(context.Context) error {
			renewed.Add(1)
			return renewErr
		},
	}
}

// actingTracker builds a tracker writing as ops, with gh scripted.
func actingTracker(t *testing.T, renewed *atomic.Int32, script ...reply) (*Tracker, *fakeGh) {
	t.Helper()
	tr, gh := build(t, script...)
	tr.ActAs(opsWriter(renewed, nil), []string{opsLogin})
	return tr, gh
}

// countAs returns how many recorded calls starting with prefix ran as as.
func countAs(gh *fakeGh, as string, prefix ...string) int {
	n := 0
	for _, c := range gh.commandsTo(prefix...) {
		if runsAs(c) == as {
			n++
		}
	}
	return n
}

// isWrite reports whether the gh arguments args write to GitHub.
func isWrite(args []string) bool {
	return slices.Equal(args[:2], []string{"label", "create"}) || slices.Equal(args[:2], []string{"issue", "edit"}) ||
		slices.Equal(args[:2], []string{"pr", "edit"}) ||
		len(args) > 2 && args[0] == "api" && args[1] == "--method" && args[2] != "GET"
}

// Covers AE1: every write runs as the writer, through its directory and
// without the boss's tokens, and every read as the boss.
func TestTheTrackerReadsAsTheBossAndWritesAsTheMate(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: []string{"auth", "status"}}, login, noCodeowners,
		reply{prefix: []string{"label", "list"}, stdout: `[{"name":"ready"}]`},
		reply{prefix: []string{"label", "create"}},
		reply{prefix: []string{"issue", "view"}, stdout: `{"state":"OPEN","labels":[{"name":"ready"}]}`},
		reply{prefix: []string{"issue", "edit"}},
		reply{prefix: []string{"api", "graphql"}, stdout: listJSON(nil, nil)},
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, stdout: "101\n"},
		reply{prefix: editComment},
		reply{prefix: commentOn(12), stdout: "900\n"},
	)
	ctx := context.Background()
	if err := tr.Prepare(ctx, []crew.State{ready, inProgress}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := tr.List(ctx, []crew.State{ready}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := tr.Move(ctx, "74", ready, inProgress); err != nil {
		t.Fatalf("Move: %v", err)
	}
	for range 2 {
		if err := tr.ReportStatus(ctx, running74(time.Time{}, "")); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	if err := tr.ReportFailure(ctx, crew.FailureReport{IssueKey: "12", IssueRef: "#12",
		Failures: []crew.ActionFailure{{Action: "lfg"}}}); err != nil {
		t.Fatalf("ReportFailure: %v", err)
	}
	writes := checkWritesAsOps(t, gh)
	// Two labels, the move, the status comment and its edit, and the report.
	if writes != 6 || renewed.Load() != 0 {
		t.Errorf("wrote %d times as ops, renewed %d times; want 6 and 0", writes, renewed.Load())
	}
}

// checkWritesAsOps checks that every recorded write ran as ops and every
// read as the boss, and returns how many writes there were.
func checkWritesAsOps(t *testing.T, gh *fakeGh) int {
	t.Helper()
	writes := 0
	for _, c := range gh.commandsTo() {
		switch {
		case !isWrite(c.Args):
			if runsAs(c) != asBoss {
				t.Errorf("read %q ran with %q, unset %q; want the boss's", c.Args, c.Env, c.Unset)
			}
		case !slices.Equal(c.Env, []string{"GH_CONFIG_DIR=/run/crew-mates/ops/crew"}) ||
			!slices.Equal(c.Unset, []string{"GH_TOKEN", "GITHUB_TOKEN"}):
			t.Errorf("write %q ran with %q, unset %q; want ops's", c.Args, c.Env, c.Unset)
		default:
			writes++
		}
	}
	return writes
}

// Covers AE4: without ActAs, and without CODEOWNERS, everything runs as the
// boss and the poll asks for the issues of gh's login alone.
func TestWithoutAMateEverythingRunsAsTheBoss(t *testing.T) {
	tr, gh := build(t,
		reply{prefix: []string{"auth", "status"}}, login, noCodeowners,
		reply{prefix: []string{"label", "list"}, stdout: `[{"name":"ready"},{"name":"waiting brainstorm"}]`},
		reply{prefix: []string{"api", "graphql"}, stdout: listJSON(nil, nil)},
		reply{prefix: []string{"issue", "view"}, stdout: `{"state":"OPEN","labels":[{"name":"ready"}]}`},
		reply{prefix: []string{"issue", "edit"}},
	)
	ctx := context.Background()
	if err := tr.Prepare(ctx, []crew.State{ready}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := tr.List(ctx, []crew.State{ready}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := tr.Move(ctx, "74", ready, inProgress); err != nil {
		t.Fatalf("Move: %v", err)
	}
	for _, c := range gh.commandsTo() {
		if c.Env != nil || c.Unset != nil {
			t.Errorf("%q ran with %q, unset %q; want neither", c.Args, c.Env, c.Unset)
		}
	}
	q := gh.callsTo("api", "graphql")[0]
	if !slices.Equal(fieldValues(q, "author0"), []string{"me"}) || fieldValues(q, "author1") != nil {
		t.Errorf("query authors = %q, %q; want me alone", fieldValues(q, "author0"), fieldValues(q, "author1"))
	}
	if boss := tr.Boss(); !slices.Equal(boss, []string{"me"}) {
		t.Errorf("Boss = %q, want gh's login", boss)
	}
}

func TestAMateRefusedAPermissionHandsEveryLaterWriteToTheBoss(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: []string{"issue", "view"}, stdout: `{"state":"OPEN","labels":[{"name":"ready"}]}`},
		reply{prefix: []string{"issue", "edit"}, as: asMate,
			stderr: "GraphQL: Resource not accessible by integration (addLabelsToLabelable)"},
		reply{prefix: []string{"issue", "edit"}, as: asBoss},
	)
	for range 2 {
		if err := tr.Move(context.Background(), "74", ready, inProgress); err != nil {
			t.Fatalf("Move: %v", err)
		}
	}
	if mate, boss := countAs(gh, asMate, "issue", "edit"), countAs(gh, asBoss, "issue", "edit"); mate != 1 || boss != 2 {
		t.Errorf("edited %d times as ops and %d as the boss, want 1 and 2", mate, boss)
	}
	if renewed.Load() != 0 {
		t.Errorf("renewed %d times, want none for a permission", renewed.Load())
	}
}

func TestAMateRefusedItsTokenRenewsItAndTriesAgain(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: editComment, as: asMate, once: true, stderr: "gh: Bad credentials (HTTP 401)"},
		reply{prefix: editComment, as: asMate},
	)
	tr.rememberStatus("74", cachedStatus{id: 101, body: "status", author: opsLogin})
	for range 2 {
		if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	if renewed.Load() != 1 || countAs(gh, asMate, editComment...) != 3 || countAs(gh, asBoss, editComment...) != 0 {
		t.Errorf("renewed %d times and edited %d times as ops, %d as the boss; want 1, 3 and 0",
			renewed.Load(), countAs(gh, asMate, editComment...), countAs(gh, asBoss, editComment...))
	}
}

func TestAMateStillRefusedAfterRenewingHandsEveryLaterWriteToTheBoss(t *testing.T) {
	for name, tc := range map[string]struct {
		renewErr error
		wantMate int
	}{
		"renewed":        {wantMate: 2},
		"renewal failed": {renewErr: errors.New("GitHub answered 502"), wantMate: 1},
	} {
		t.Run(name, func(t *testing.T) {
			var renewed atomic.Int32
			tr, gh := build(t, login,
				reply{prefix: editComment, as: asMate, stderr: "gh: Bad credentials (HTTP 401)"},
				reply{prefix: editComment, as: asBoss},
			)
			tr.ActAs(opsWriter(&renewed, tc.renewErr), []string{opsLogin})
			tr.rememberStatus("74", cachedStatus{id: 101, body: "status", author: opsLogin})
			for range 2 {
				if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
					t.Fatalf("ReportStatus: %v", err)
				}
			}
			mate, boss := countAs(gh, asMate, editComment...), countAs(gh, asBoss, editComment...)
			if mate != tc.wantMate || boss != 2 || len(gh.callsTo(createComment...)) != 0 {
				t.Errorf("edited %d times as ops and %d as the boss, created %d; want %d, 2 and none",
					mate, boss, len(gh.callsTo(createComment...)), tc.wantMate)
			}
		})
	}
}

func TestAnIssueGoneForTheBossTooKeepsTheMateWriting(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: commentOn(12), stderr: "gh: Not Found (HTTP 404)"},
	)
	report := crew.FailureReport{IssueKey: "12", IssueRef: "#12", Failures: []crew.ActionFailure{{Action: "lfg"}}}
	for range 2 {
		err := tr.ReportFailure(context.Background(), report)
		if !errors.Is(err, port.ErrMovedMeanwhile) {
			t.Fatalf("ReportFailure = %v, want ErrMovedMeanwhile", err)
		}
	}
	if mate, boss := countAs(gh, asMate, commentOn(12)...), countAs(gh, asBoss, commentOn(12)...); mate != 2 || boss != 2 {
		t.Errorf("posted %d times as ops and %d as the boss, want 2 and 2", mate, boss)
	}
}

func TestAMateThatLostAccessHandsTheStatusCommentToTheBoss(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed, login,
		reply{prefix: editComment, as: asMate, stderr: "gh: Not Found (HTTP 404)"},
		reply{prefix: editComment, as: asBoss},
	)
	tr.rememberStatus("74", cachedStatus{id: 101, body: "status", author: opsLogin})
	for range 3 {
		if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	for _, c := range gh.commandsTo(editComment...) {
		if !slices.Contains(c.Args, "repos/{owner}/{repo}/issues/comments/101") {
			t.Errorf("edited %q, want comment 101", c.Args)
		}
	}
	if mate, boss := countAs(gh, asMate, editComment...), countAs(gh, asBoss, editComment...); mate != 1 || boss != 3 {
		t.Errorf("edited %d times as ops and %d as the boss, want 1 and 3", mate, boss)
	}
	if n := len(gh.callsTo(createComment...)); n != 0 {
		t.Errorf("created %d comments, want none", n)
	}
}

func TestARateLimitedMateKeepsWriting(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: commentOn(12), as: asMate, stderr: "gh: You have exceeded a secondary rate limit. (HTTP 403)"},
	)
	report := crew.FailureReport{IssueKey: "12", IssueRef: "#12", Failures: []crew.ActionFailure{{Action: "lfg"}}}
	for range 2 {
		err := tr.ReportFailure(context.Background(), report)
		if err == nil {
			t.Fatal("ReportFailure = nil, want the rate limit")
		}
		wantClassified(t, err, nil)
	}
	if mate, boss := countAs(gh, asMate, commentOn(12)...), countAs(gh, asBoss, commentOn(12)...); mate != 2 || boss != 0 {
		t.Errorf("posted %d times as ops and %d as the boss, want 2 and 0", mate, boss)
	}
}

// Covers R21: a status comment the boss wrote is continued, not edited, by
// a mate.
func TestAMateContinuesAStatusCommentTheBossWrote(t *testing.T) {
	marked := "crew: an older status.\n\n" + statusMarker + "\n"
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed, login,
		reply{prefix: listComments, stdout: "[" + strings.Join([]string{
			commentJSON(9, "someone-else", marked),
			commentJSON(12, "me", marked),
		}, ",") + "]"},
		reply{prefix: createComment, as: asMate, stdout: "130\n"},
		reply{prefix: editComment, as: asMate},
	)
	for range 2 {
		if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	creates := gh.callsTo(createComment...)
	if len(creates) != 1 {
		t.Fatalf("created %d comments, want 1", len(creates))
	}
	body := statusBody(t, creates[0])
	if !strings.HasPrefix(body, continuesMarker+"\ncrew: this comment continues crew's earlier status comment on #74, "+
		"which another account wrote.\n\n") || !endsWithMarker(body) {
		t.Errorf("continuation =\n%s\nwant the continuation preamble", body)
	}
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/130") {
		t.Errorf("edits = %q, want one of the new comment 130", edits)
	}
}

func TestARestartedTrackerFindsTheStatusCommentAMateWrote(t *testing.T) {
	marked := "crew: an older status.\n\n" + statusMarker + "\n"
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed, login,
		reply{prefix: listComments, stdout: "[" + strings.Join([]string{
			commentJSON(12, "me", marked),
			commentJSON(14, "Crew-Ops[bot]", marked),
			commentJSON(20, "crew-ops", marked),
		}, ",") + "]"},
		reply{prefix: editComment, as: asMate},
	)
	if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
		t.Fatalf("ReportStatus: %v", err)
	}
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/14") {
		t.Errorf("edits = %q, want one of ops's comment 14", edits)
	}
}

func TestAMateRefusedEveryWriteLeavesOneStatusComment(t *testing.T) {
	refused := "gh: Resource not accessible by integration (HTTP 403)"
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed, login,
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, as: asMate, stderr: refused},
		reply{prefix: createComment, as: asBoss, stdout: "101\n"},
		reply{prefix: editComment, as: asMate, stderr: refused},
		reply{prefix: editComment, as: asBoss},
	)
	for range 3 {
		if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	if n := countAs(gh, asBoss, createComment...); n != 1 {
		t.Errorf("created %d comments as the boss, want 1", n)
	}
	if mate, boss := countAs(gh, asMate, editComment...), countAs(gh, asBoss, editComment...); mate != 0 || boss != 2 {
		t.Errorf("edited %d times as ops and %d as the boss, want 0 and 2", mate, boss)
	}
}

func TestAMateNotLoggedInHandsTheWriteToTheBoss(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: []string{"label", "create"}, as: asMate,
			stderr: "To get started with GitHub CLI, please run:  gh auth login"},
		reply{prefix: []string{"label", "create"}, as: asBoss},
	)
	if _, _, err := tr.gh.write(context.Background(), "label", "create", "ready"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if mate, boss := countAs(gh, asMate, "label"), countAs(gh, asBoss, "label"); mate != 1 || boss != 1 {
		t.Errorf("created %d times as ops and %d as the boss, want 1 and 1", mate, boss)
	}
}

func TestAWriteThatCannotStartIsTheMatesError(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: []string{"label", "create"}, err: errors.New("start gh: no such file")},
	)
	_, mate, err := tr.gh.write(context.Background(), "label", "create", "ready")
	if err == nil || mate != opsLogin {
		t.Errorf("write = %q, %v; want ops and the error", mate, err)
	}
	if n := len(gh.commandsTo()); n != 1 {
		t.Errorf("ran gh %d times, want once", n)
	}
}
