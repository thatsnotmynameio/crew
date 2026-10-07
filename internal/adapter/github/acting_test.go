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

// Who a gh call runs as: you, with no environment of its own, or a
// bot, with its gh config directory.
const (
	asYou = "you"
	asBot = "bot"
)

// runsAs returns who c runs as: asBot when it carries an environment of
// its own, asYou otherwise.
func runsAs(c proc.Command) string {
	if len(c.Env) > 0 || len(c.Unset) > 0 {
		return asBot
	}
	return asYou
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

// The bot ops, crew's writer in the tests below.
const opsLogin = "crew-ops[bot]"

// The warnings the tracker reports when ops's writes went back to you,
// one per refusal kind.
const (
	lostCredentials = "crew's writes as bot ops went back to you: GitHub refused its credentials, " +
		"as when its token was revoked or expired; restart crew, and run `crew bots create ops` " +
		"in this repository if it happens again; crew writes as you until it restarts"
	lostPermission = "crew's writes as bot ops went back to you: GitHub refused it a permission; " +
		"run `crew bots create ops` in this repository; crew writes as you until it restarts"
	lostAccess = "crew's writes as bot ops went back to you: it lost access to the repository; " +
		"run `crew bots create ops` in this repository to install it; crew writes as you until it restarts"
)

// wantWriterLost checks that tr reports want as its writes warning.
func wantWriterLost(t *testing.T, tr *Tracker, want string) {
	t.Helper()
	var reporter port.WriterReporter = tr
	if got := reporter.WriterLost(); got != want {
		t.Errorf("WriterLost =\n%q\nwant\n%q", got, want)
	}
}

// opsWriter returns ops as the writer, whose Renew counts its calls in
// renewed and fails when renewErr is set.
func opsWriter(renewed *atomic.Int32, renewErr error) port.Identity {
	return port.Identity{
		Bot: "ops", Login: opsLogin,
		Env:   []string{"GH_CONFIG_DIR=/run/crew-bots/ops/crew"},
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
// without your tokens, and every read as you.
func TestTheTrackerReadsAsYouAndWritesAsTheBot(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: []string{"auth", "status"}}, login, noCodeowners,
		repositoryReply,
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
	wantWriterLost(t, tr, "")
	if err := tr.Prepare(ctx, []crew.State{ready, inProgress}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := tr.List(ctx, []crew.State{ready}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := tr.Move(ctx, issueID("74"), ready, inProgress); err != nil {
		t.Fatalf("Move: %v", err)
	}
	for range 2 {
		if err := tr.ReportStatus(ctx, running74(time.Time{}, "")); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	if err := tr.ReportFailure(ctx, crew.FailureReport{IssueID: issueID("12"), IssueRef: "#12",
		Failures: []crew.ActionFailure{{Action: "lfg"}}}); err != nil {
		t.Fatalf("ReportFailure: %v", err)
	}
	writes := checkWritesAsOps(t, gh)
	// The missing label, the move, the status comment and its edit, and the
	// report.
	if writes != 5 || renewed.Load() != 0 {
		t.Errorf("wrote %d times as ops, renewed %d times; want 5 and 0", writes, renewed.Load())
	}
	wantWriterLost(t, tr, "")
}

// checkWritesAsOps checks that every recorded write ran as ops and every
// read as you, and returns how many writes there were.
func checkWritesAsOps(t *testing.T, gh *fakeGh) int {
	t.Helper()
	writes := 0
	for _, c := range gh.commandsTo() {
		switch {
		case !isWrite(c.Args):
			if runsAs(c) != asYou {
				t.Errorf("read %q ran with %q, unset %q; want yours", c.Args, c.Env, c.Unset)
			}
		case !slices.Equal(c.Env, []string{"GH_CONFIG_DIR=/run/crew-bots/ops/crew"}) ||
			!slices.Equal(c.Unset, []string{"GH_TOKEN", "GITHUB_TOKEN"}):
			t.Errorf("write %q ran with %q, unset %q; want ops's", c.Args, c.Env, c.Unset)
		default:
			writes++
		}
	}
	return writes
}

// Covers AE4: without ActAs, and without CODEOWNERS, everything runs as the
// you and the poll asks for the issues of gh's login alone.
func TestWithoutABotEverythingRunsAsYou(t *testing.T) {
	tr, gh := build(t,
		reply{prefix: []string{"auth", "status"}}, login, noCodeowners,
		repositoryReply,
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
	if err := tr.Move(ctx, issueID("74"), ready, inProgress); err != nil {
		t.Fatalf("Move: %v", err)
	}
	for _, c := range gh.commandsTo() {
		if c.Env != nil || c.Unset != nil {
			t.Errorf("%q ran with %q, unset %q; want neither", c.Args, c.Env, c.Unset)
		}
	}
	graphql := gh.callsTo("api", "graphql")
	q := graphql[len(graphql)-1] // the listing, after Prepare's repository read
	if !slices.Equal(fieldValues(q, "author0"), []string{"me"}) || fieldValues(q, "author1") != nil {
		t.Errorf("query authors = %q, %q; want me alone", fieldValues(q, "author0"), fieldValues(q, "author1"))
	}
	if codeOwners := tr.CodeOwners(); !slices.Equal(codeOwners, []string{"me"}) {
		t.Errorf("CodeOwners = %q, want gh's login", codeOwners)
	}
}

func TestABotRefusedAPermissionHandsEveryLaterWriteToYou(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: []string{"issue", "view"}, stdout: `{"state":"OPEN","labels":[{"name":"ready"}]}`},
		reply{prefix: []string{"issue", "edit"}, as: asBot,
			stderr: "GraphQL: Resource not accessible by integration (addLabelsToLabelable)"},
		reply{prefix: []string{"issue", "edit"}, as: asYou},
	)
	for range 2 {
		if err := tr.Move(context.Background(), issueID("74"), ready, inProgress); err != nil {
			t.Fatalf("Move: %v", err)
		}
	}
	if bot, you := countAs(gh, asBot, "issue", "edit"), countAs(gh, asYou, "issue", "edit"); bot != 1 || you != 2 {
		t.Errorf("edited %d times as ops and %d as you, want 1 and 2", bot, you)
	}
	if renewed.Load() != 0 {
		t.Errorf("renewed %d times, want none for a permission", renewed.Load())
	}
	wantWriterLost(t, tr, lostPermission)
}

func TestABotRefusedItsTokenRenewsItAndTriesAgain(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: editComment, as: asBot, once: true, stderr: "gh: Bad credentials (HTTP 401)"},
		reply{prefix: editComment, as: asBot},
	)
	tr.rememberStatus("74", cachedStatus{id: 101, body: "status", author: opsLogin})
	for range 2 {
		if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	if renewed.Load() != 1 || countAs(gh, asBot, editComment...) != 3 || countAs(gh, asYou, editComment...) != 0 {
		t.Errorf("renewed %d times and edited %d times as ops, %d as you; want 1, 3 and 0",
			renewed.Load(), countAs(gh, asBot, editComment...), countAs(gh, asYou, editComment...))
	}
	wantWriterLost(t, tr, "")
}

func TestABotStillRefusedAfterRenewingHandsEveryLaterWriteToYou(t *testing.T) {
	for name, tc := range map[string]struct {
		renewErr error
		wantBot  int
	}{
		"renewed":        {wantBot: 2},
		"renewal failed": {renewErr: errors.New("GitHub answered 502"), wantBot: 1},
	} {
		t.Run(name, func(t *testing.T) {
			var renewed atomic.Int32
			tr, gh := build(t, login,
				reply{prefix: editComment, as: asBot, stderr: "gh: Bad credentials (HTTP 401)"},
				reply{prefix: editComment, as: asYou},
			)
			tr.ActAs(opsWriter(&renewed, tc.renewErr), []string{opsLogin})
			tr.rememberStatus("74", cachedStatus{id: 101, body: "status", author: opsLogin})
			for range 2 {
				if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
					t.Fatalf("ReportStatus: %v", err)
				}
			}
			bot, you := countAs(gh, asBot, editComment...), countAs(gh, asYou, editComment...)
			if bot != tc.wantBot || you != 2 || len(gh.callsTo(createComment...)) != 0 {
				t.Errorf("edited %d times as ops and %d as you, created %d; want %d, 2 and none",
					bot, you, len(gh.callsTo(createComment...)), tc.wantBot)
			}
			wantWriterLost(t, tr, lostCredentials)
		})
	}
}

func TestAnIssueGoneForYouTooKeepsTheBotWriting(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: commentOn(12), stderr: "gh: Not Found (HTTP 404)"},
	)
	report := crew.FailureReport{IssueID: issueID("12"), IssueRef: "#12", Failures: []crew.ActionFailure{{Action: "lfg"}}}
	for range 2 {
		err := tr.ReportFailure(context.Background(), report)
		if !errors.Is(err, port.ErrMovedMeanwhile) {
			t.Fatalf("ReportFailure = %v, want ErrMovedMeanwhile", err)
		}
	}
	if bot, you := countAs(gh, asBot, commentOn(12)...), countAs(gh, asYou, commentOn(12)...); bot != 2 || you != 2 {
		t.Errorf("posted %d times as ops and %d as you, want 2 and 2", bot, you)
	}
	wantWriterLost(t, tr, "")
}

func TestABotThatLostAccessHandsTheStatusCommentToYou(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed, login,
		reply{prefix: editComment, as: asBot, stderr: "gh: Not Found (HTTP 404)"},
		reply{prefix: editComment, as: asYou},
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
	if bot, you := countAs(gh, asBot, editComment...), countAs(gh, asYou, editComment...); bot != 1 || you != 3 {
		t.Errorf("edited %d times as ops and %d as you, want 1 and 3", bot, you)
	}
	if n := len(gh.callsTo(createComment...)); n != 0 {
		t.Errorf("created %d comments, want none", n)
	}
	wantWriterLost(t, tr, lostAccess)
}

func TestARateLimitedBotKeepsWriting(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: commentOn(12), as: asBot, stderr: "gh: You have exceeded a secondary rate limit. (HTTP 403)"},
	)
	report := crew.FailureReport{IssueID: issueID("12"), IssueRef: "#12", Failures: []crew.ActionFailure{{Action: "lfg"}}}
	for range 2 {
		err := tr.ReportFailure(context.Background(), report)
		if err == nil {
			t.Fatal("ReportFailure = nil, want the rate limit")
		}
		wantClassified(t, err, nil)
	}
	if bot, you := countAs(gh, asBot, commentOn(12)...), countAs(gh, asYou, commentOn(12)...); bot != 2 || you != 0 {
		t.Errorf("posted %d times as ops and %d as you, want 2 and 0", bot, you)
	}
	wantWriterLost(t, tr, "")
}

// Covers R21: a status comment you wrote is continued, not edited, by
// a bot.
func TestABotContinuesAStatusCommentYouWrote(t *testing.T) {
	marked := "crew: an older status.\n\n" + statusMarker + "\n"
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed, login,
		reply{prefix: listComments, stdout: "[" + strings.Join([]string{
			commentJSON(9, "someone-else", marked),
			commentJSON(12, "me", marked),
		}, ",") + "]"},
		reply{prefix: createComment, as: asBot, stdout: "130\n"},
		reply{prefix: editComment, as: asBot},
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

func TestARestartedTrackerFindsTheStatusCommentABotWrote(t *testing.T) {
	marked := "crew: an older status.\n\n" + statusMarker + "\n"
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed, login,
		reply{prefix: listComments, stdout: "[" + strings.Join([]string{
			commentJSON(12, "me", marked),
			commentJSON(14, "Crew-Ops[bot]", marked),
			commentJSON(20, "crew-ops", marked),
		}, ",") + "]"},
		reply{prefix: editComment, as: asBot},
	)
	if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
		t.Fatalf("ReportStatus: %v", err)
	}
	edits := gh.callsTo(editComment...)
	if len(edits) != 1 || !slices.Contains(edits[0], "repos/{owner}/{repo}/issues/comments/14") {
		t.Errorf("edits = %q, want one of ops's comment 14", edits)
	}
}

func TestABotRefusedEveryWriteLeavesOneStatusComment(t *testing.T) {
	refused := "gh: Resource not accessible by integration (HTTP 403)"
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed, login,
		reply{prefix: listComments, stdout: "[]"},
		reply{prefix: createComment, as: asBot, stderr: refused},
		reply{prefix: createComment, as: asYou, stdout: "101\n"},
		reply{prefix: editComment, as: asBot, stderr: refused},
		reply{prefix: editComment, as: asYou},
	)
	for range 3 {
		if err := tr.ReportStatus(context.Background(), running74(time.Time{}, "")); err != nil {
			t.Fatalf("ReportStatus: %v", err)
		}
	}
	if n := countAs(gh, asYou, createComment...); n != 1 {
		t.Errorf("created %d comments as you, want 1", n)
	}
	if bot, you := countAs(gh, asBot, editComment...), countAs(gh, asYou, editComment...); bot != 0 || you != 2 {
		t.Errorf("edited %d times as ops and %d as you, want 0 and 2", bot, you)
	}
}

func TestABotNotLoggedInHandsTheWriteToYou(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: []string{"label", "create"}, as: asBot,
			stderr: "To get started with GitHub CLI, please run:  gh auth login"},
		reply{prefix: []string{"label", "create"}, as: asYou},
	)
	if _, _, err := tr.gh.write(context.Background(), "label", "create", "ready"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if bot, you := countAs(gh, asBot, "label"), countAs(gh, asYou, "label"); bot != 1 || you != 1 {
		t.Errorf("created %d times as ops and %d as you, want 1 and 1", bot, you)
	}
	wantWriterLost(t, tr, lostPermission)
}

func TestAWriteThatCannotStartIsTheBotsError(t *testing.T) {
	var renewed atomic.Int32
	tr, gh := actingTracker(t, &renewed,
		reply{prefix: []string{"label", "create"}, err: errors.New("start gh: no such file")},
	)
	_, bot, err := tr.gh.write(context.Background(), "label", "create", "ready")
	if err == nil || bot != opsLogin {
		t.Errorf("write = %q, %v; want ops and the error", bot, err)
	}
	if n := len(gh.commandsTo()); n != 1 {
		t.Errorf("ran gh %d times, want once", n)
	}
}

// Two writes that fall back at once, as two goroutines whose bot both
// refused before either went back to you, keep the first one's warning.
func TestTwoWritesFallingBackKeepTheFirstWarning(t *testing.T) {
	var renewed atomic.Int32
	tr, _ := actingTracker(t, &renewed)
	tr.gh.backToLogin(refusedPermission, "ops")
	tr.gh.backToLogin(refusedNotFound, "ops")
	wantWriterLost(t, tr, lostPermission)
}

// Without a bot, your writes never fall back, so no warning comes.
func TestYouWritingAloneReportsNoWritesWarning(t *testing.T) {
	tr, _ := build(t, reply{prefix: []string{"label", "create"}, stderr: "gh: Resource not accessible (HTTP 403)"})
	if _, _, err := tr.gh.write(context.Background(), "label", "create", "ready"); err == nil {
		t.Fatal("write = nil, want your refusal")
	}
	wantWriterLost(t, tr, "")
}

func TestTheTrackerFindsItsLoginInPrepare(t *testing.T) {
	tr, _ := build(t,
		reply{prefix: []string{"auth", "status"}}, login,
		reply{prefix: []string{"api", "-H", rawAccept, "repos/{owner}/{repo}/contents/.github/CODEOWNERS"},
			stdout: "* @octocat\n"},
		repositoryReply,
		reply{prefix: []string{"label", "list"}, stdout: `[{"name":"ready"},{"name":"waiting brainstorm"}]`},
	)
	var finder port.LoginFinder = tr
	if got := finder.Login(); got != "" {
		t.Errorf("Login before Prepare = %q, want none", got)
	}
	if err := tr.Prepare(context.Background(), []crew.State{ready}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := finder.Login(); got != "me" {
		t.Errorf("Login = %q, want gh's login me, not CODEOWNERS' octocat", got)
	}
}
