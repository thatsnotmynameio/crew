package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/config"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// reply is a scripted answer to every gh call whose arguments start with
// prefix, as the old dispatcher's FakeGh scripted them.
type reply struct {
	prefix []string
	stdout string
	// stderr, when set, makes the call exit non-zero with it.
	stderr string
	// err, when set, is returned as is, as when gh cannot start.
	err error
}

// fakeGh is the injected runner: it answers gh calls from a script, records
// every call, and fails the test on a call nothing scripted.
type fakeGh struct {
	t      *testing.T
	script []reply

	mu    sync.Mutex
	calls [][]string
}

func newFakeGh(t *testing.T, script ...reply) *fakeGh {
	return &fakeGh{t: t, script: script}
}

func (f *fakeGh) run(_ context.Context, c proc.Command) (proc.Output, error) {
	f.mu.Lock()
	f.calls = append(f.calls, slices.Clone(c.Args))
	f.mu.Unlock()
	if c.Name != "gh" {
		f.t.Errorf("ran %q, want only gh", c.Name)
		return proc.Output{}, errors.New("not gh")
	}
	for _, r := range f.script {
		if len(c.Args) < len(r.prefix) || !slices.Equal(c.Args[:len(r.prefix)], r.prefix) {
			continue
		}
		switch {
		case r.err != nil:
			return proc.Output{}, r.err
		case r.stderr != "":
			// The shape proc.Group.Run gives a non-zero exit.
			return proc.Output{Stdout: []byte(r.stdout), Stderr: []byte(r.stderr)},
				fmt.Errorf("gh: exit status 1: %s", r.stderr)
		}
		return proc.Output{Stdout: []byte(r.stdout)}, nil
	}
	f.t.Errorf("unscripted gh call: %q", c.Args)
	return proc.Output{}, errors.New("unscripted gh call")
}

// callsTo returns the recorded calls starting with prefix.
func (f *fakeGh) callsTo(prefix ...string) [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, c := range f.calls {
		if len(c) >= len(prefix) && slices.Equal(c[:len(prefix)], prefix) {
			out = append(out, c)
		}
	}
	return out
}

// oneStage is a minimal valid workflow, so the files below load.
const oneStage = `workflow:
  - name: implement
    label: ready
    moves_to: in_progress
    on_success: ready_to_review
    on_failure: needs_attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
`

// section loads a .crew/config.yaml holding tracker, which is the tracker:
// section's body, and returns the section's strict decoder.
func section(t *testing.T, tracker string) port.Decode {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	body := "tracker:\n  name: github\n" + tracker + oneStage
	if err := os.WriteFile(filepath.Join(root, ".crew", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg.TrackerSection
}

// build builds the tracker from the tracker section's body, with gh scripted.
func build(t *testing.T, tracker string, script ...reply) (*Tracker, *fakeGh) {
	t.Helper()
	gh := newFakeGh(t, script...)
	tr, err := factory(gh.run)(section(t, tracker))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	return tr.(*Tracker), gh
}

var login = reply{prefix: []string{"api", "user"}, stdout: "me\n"}

func issuesJSON(nodes ...string) string {
	return `{"data":{"repository":{"issues":{"nodes":[` + strings.Join(nodes, ",") + `]}}}}`
}

func issueNode(number int, created string, labels ...string) string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = fmt.Sprintf(`{"name":%q}`, l)
	}
	return fmt.Sprintf(`{"number":%d,"title":"Issue %d","url":"https://github.com/o/r/issues/%d","createdAt":%q,"labels":{"nodes":[%s]}}`,
		number, number, number, created, strings.Join(names, ","))
}

// fieldValues returns the values of every -f/-F field named key in args.
func fieldValues(args []string, key string) []string {
	var out []string
	for i := 0; i+1 < len(args); i++ {
		if (args[i] == "-f" || args[i] == "-F") && strings.HasPrefix(args[i+1], key+"=") {
			out = append(out, strings.TrimPrefix(args[i+1], key+"="))
		}
	}
	return out
}

func TestListSendsOneQueryFilteredByLoginAndLabels(t *testing.T) {
	tr, gh := build(t, "", login, reply{
		prefix: []string{"api", "graphql"},
		stdout: issuesJSON(
			issueNode(12, "2026-09-01T10:00:00Z", "ready", "bug"),
			issueNode(14, "2026-09-02T10:00:00Z", "ready to review"),
		),
	})

	got, err := tr.List(context.Background(), []crew.State{crew.Ready, crew.ReadyToReview})
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	queries := gh.callsTo("api", "graphql")
	if len(queries) != 1 {
		t.Fatalf("sent %d GraphQL queries, want 1", len(queries))
	}
	q := queries[0]
	if logins := fieldValues(q, "login"); !slices.Equal(logins, []string{"me"}) {
		t.Errorf("login variable = %q, want [me]", logins)
	}
	if labels := fieldValues(q, "labels[]"); !slices.Equal(labels, []string{"ready", "ready to review"}) {
		t.Errorf("labels variable = %q, want [ready, ready to review]", labels)
	}
	query := strings.Join(fieldValues(q, "query"), "")
	for _, want := range []string{"createdBy: $login", "labels: $labels", "states: OPEN", "CREATED_AT", "ASC"} {
		if !strings.Contains(query, want) {
			t.Errorf("query does not contain %q:\n%s", want, query)
		}
	}

	want := []crew.Issue{
		{Key: "12", Ref: "#12", Title: "Issue 12", URL: "https://github.com/o/r/issues/12",
			Created: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), States: []crew.State{crew.Ready}},
		{Key: "14", Ref: "#14", Title: "Issue 14", URL: "https://github.com/o/r/issues/14",
			Created: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), States: []crew.State{crew.ReadyToReview}},
	}
	if len(got) != len(want) {
		t.Fatalf("List = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Key != want[i].Key || got[i].Ref != want[i].Ref || got[i].Title != want[i].Title ||
			got[i].URL != want[i].URL || !got[i].Created.Equal(want[i].Created) || !slices.Equal(got[i].States, want[i].States) {
			t.Errorf("issue %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestListResolvesTheLoginOnce(t *testing.T) {
	tr, gh := build(t, "", login, reply{prefix: []string{"api", "graphql"}, stdout: issuesJSON()})
	for range 2 {
		if _, err := tr.List(context.Background(), []crew.State{crew.Ready}); err != nil {
			t.Fatalf("List: %v", err)
		}
	}
	if n := len(gh.callsTo("api", "user")); n != 1 {
		t.Errorf("asked gh for the login %d times, want 1", n)
	}
}

func TestListReturnsEveryCrewStateOfAnIssueWhateverTheCase(t *testing.T) {
	tr, _ := build(t, "", login, reply{
		prefix: []string{"api", "graphql"},
		stdout: issuesJSON(issueNode(4, "2026-09-01T10:00:00Z", "ready", "Needs Attention", "bug")),
	})
	got, err := tr.List(context.Background(), []crew.State{crew.Ready})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || !slices.Equal(got[0].States, []crew.State{crew.Ready, crew.NeedsAttention}) {
		t.Errorf("List = %+v, want #4 in ready and needs_attention", got)
	}
}

func TestListUsesTheConfiguredLabels(t *testing.T) {
	tr, gh := build(t, "  labels:\n    ready: todo\n", login, reply{
		prefix: []string{"api", "graphql"},
		stdout: issuesJSON(issueNode(5, "2026-09-01T10:00:00Z", "todo")),
	})
	got, err := tr.List(context.Background(), []crew.State{crew.Ready, crew.InProgress})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	queries := gh.callsTo("api", "graphql")
	if len(queries) != 1 {
		t.Fatalf("sent %d GraphQL queries, want 1", len(queries))
	}
	if labels := fieldValues(queries[0], "labels[]"); !slices.Equal(labels, []string{"todo", "in progress"}) {
		t.Errorf("labels variable = %q, want [todo, in progress]", labels)
	}
	if len(got) != 1 || !slices.Equal(got[0].States, []crew.State{crew.Ready}) {
		t.Errorf("List = %+v, want #5 in ready", got)
	}
}

func TestMoveSwapsTheCrewLabelsInOneEdit(t *testing.T) {
	tr, gh := build(t, "",
		reply{prefix: []string{"issue", "view", "3"}, stdout: `{"state":"OPEN","labels":[{"name":"ready"},{"name":"bug"},{"name":"Needs Attention"}]}`},
		reply{prefix: []string{"issue", "edit", "3"}},
	)
	if err := tr.Move(context.Background(), "3", crew.Ready, crew.InProgress); err != nil {
		t.Fatalf("Move: %v", err)
	}
	edits := gh.callsTo("issue", "edit")
	want := []string{"issue", "edit", "3", "--remove-label=ready", "--remove-label=Needs Attention", "--add-label=in progress"}
	if len(edits) != 1 || !slices.Equal(edits[0], want) {
		t.Errorf("edits = %q, want one: %q", edits, want)
	}
}

func TestMoveOfAnIssueThatMovedMeanwhileEditsNothing(t *testing.T) {
	for name, view := range map[string]string{
		"closed":          `{"state":"CLOSED","labels":[{"name":"ready"}]}`,
		"no longer ready": `{"state":"OPEN","labels":[{"name":"needs attention"}]}`,
		"in no state":     `{"state":"OPEN","labels":[{"name":"bug"}]}`,
		"in to and other": `{"state":"OPEN","labels":[{"name":"in progress"},{"name":"paused"}]}`,
		"closed in to":    `{"state":"CLOSED","labels":[{"name":"in progress"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t, "", reply{prefix: []string{"issue", "view", "3"}, stdout: view})
			err := tr.Move(context.Background(), "3", crew.Ready, crew.InProgress)
			if !errors.Is(err, port.ErrMovedMeanwhile) {
				t.Errorf("Move = %v, want ErrMovedMeanwhile", err)
			}
			if edits := gh.callsTo("issue", "edit"); len(edits) != 0 {
				t.Errorf("sent edits %q, want none", edits)
			}
		})
	}
}

// A retry of a move that landed although gh reported an error finds the issue
// already in to: the move is done, and nothing is edited again (KTD8).
func TestMoveOfAnIssueAlreadyInToIsDoneWithoutAnEdit(t *testing.T) {
	tr, gh := build(t, "",
		reply{prefix: []string{"issue", "view", "3"}, stdout: `{"state":"OPEN","labels":[{"name":"In Progress"},{"name":"bug"}]}`},
	)
	if err := tr.Move(context.Background(), "3", crew.Ready, crew.InProgress); err != nil {
		t.Fatalf("Move = %v, want nil", err)
	}
	if edits := gh.callsTo("issue", "edit"); len(edits) != 0 {
		t.Errorf("sent edits %q, want none", edits)
	}
}

func TestMoveToAMissingLabelIsRefused(t *testing.T) {
	tr, _ := build(t, "",
		reply{prefix: []string{"issue", "view", "3"}, stdout: `{"state":"OPEN","labels":[{"name":"ready"}]}`},
		reply{prefix: []string{"issue", "edit", "3"}, stderr: "could not add label: 'in progress' not found\n"},
	)
	err := tr.Move(context.Background(), "3", crew.Ready, crew.InProgress)
	if !errors.Is(err, port.ErrRefused) {
		t.Errorf("Move = %v, want ErrRefused", err)
	}
}

func TestAFailingGhCallIsTransientAndCarriesItsStderr(t *testing.T) {
	tr, _ := build(t, "",
		reply{prefix: []string{"issue", "view", "3"}, stderr: "HTTP 502: Bad Gateway"},
		reply{prefix: []string{"issue", "comment", "3"}, stderr: "HTTP 502: Bad Gateway"},
		reply{prefix: []string{"api", "user"}, stderr: "HTTP 502: Bad Gateway"},
	)
	errs := map[string]error{
		"Move": tr.Move(context.Background(), "3", crew.Ready, crew.InProgress),
		"ReportFailure": tr.ReportFailure(context.Background(), crew.FailureReport{
			IssueKey: "3", IssueRef: "#3", Failures: []crew.ActionFailure{{Action: "development"}},
		}),
	}
	_, errs["List"] = tr.List(context.Background(), []crew.State{crew.Ready})
	for name, err := range errs {
		if err == nil || !strings.Contains(err.Error(), "HTTP 502: Bad Gateway") {
			t.Errorf("%s = %v, want an error carrying gh's stderr", name, err)
		}
		if errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused) {
			t.Errorf("%s = %v, want a transient error", name, err)
		}
	}
}

func TestLabelsMappingTwoStatesToOneNameFailNamingBoth(t *testing.T) {
	_, err := factory(newFakeGh(t).run)(section(t, "  labels:\n    done: Review\n    in_review: review\n"))
	if err == nil {
		t.Fatal("factory succeeded, want an error")
	}
	for _, want := range []string{"tracker.labels", "done", "in_review"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestUnknownTrackerKeysFailNamingThem(t *testing.T) {
	for name, tc := range map[string]struct{ tracker, want string }{
		"section key": {"  lables:\n    ready: todo\n", "tracker.lables"},
		"state key":   {"  labels:\n    redy: todo\n", "tracker.labels.redy"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := factory(newFakeGh(t).run)(section(t, tc.tracker))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("factory error = %v, want one naming %s", err, tc.want)
			}
		})
	}
}

// fenced returns the content of each fenced code block in markdown, checking
// that each closes with exactly its own opening fence.
func fenced(t *testing.T, markdown string) []string {
	t.Helper()
	var blocks []string
	lines := strings.Split(markdown, "\n")
	for i := 0; i < len(lines); i++ {
		fence := strings.TrimRight(lines[i], "abcdefghijklmnopqrstuvwxyz")
		if len(fence) < 3 || strings.Trim(fence, "`") != "" {
			continue
		}
		end := slices.IndexFunc(lines[i+1:], func(l string) bool {
			return strings.Trim(l, "`") == "" && len(l) >= len(fence)
		})
		if end < 0 {
			t.Fatalf("fence on line %d never closes:\n%s", i+1, markdown)
		}
		blocks = append(blocks, strings.Join(lines[i+1:i+1+end], "\n"))
		i += end + 1
	}
	return blocks
}

func TestReportFailureFencesEachReason(t *testing.T) {
	tr, gh := build(t, "", reply{prefix: []string{"issue", "comment", "12"}})
	reason := "ask @someone why ```make``` failed"
	err := tr.ReportFailure(context.Background(), crew.FailureReport{
		IssueKey: "12", IssueRef: "#12",
		Failures: []crew.ActionFailure{
			{Action: "development", Reason: reason, Workspace: "issue-12-development", Log: ".crew/logs/issue-12-development.log"},
			{Action: "acceptance", Reason: "tests did not build", Workspace: "issue-12-acceptance", Log: ".crew/logs/issue-12-acceptance.log"},
			// A reason that is a fence of its own must not close its block.
			{Action: "lint", Reason: "````", Workspace: "issue-12-lint", Log: ".crew/logs/issue-12-lint.log"},
		},
	})
	if err != nil {
		t.Fatalf("ReportFailure: %v", err)
	}
	comments := gh.callsTo("issue", "comment", "12")
	if len(comments) != 1 {
		t.Fatalf("posted %d comments, want 1", len(comments))
	}
	args := comments[0]
	body, ok := strings.CutPrefix(args[len(args)-1], "--body=")
	if !ok {
		t.Fatalf("comment args %q do not end with --body=", args)
	}

	if blocks := fenced(t, body); !slices.Equal(blocks, []string{reason, "tests did not build", "````"}) {
		t.Errorf("fenced blocks = %q, want each reason in its own:\n%s", blocks, body)
	}
	if n := strings.Count(body, "@someone"); n != 1 {
		t.Errorf("@someone appears %d times, want once, inside its fence:\n%s", n, body)
	}
	for _, want := range []string{"development", "issue-12-development", ".crew/logs/issue-12-development.log",
		"acceptance", "issue-12-acceptance", ".crew/logs/issue-12-acceptance.log"} {
		if !strings.Contains(body, want) {
			t.Errorf("comment does not name %q:\n%s", want, body)
		}
	}
}

func TestPrepareWithoutAuthTellsTheBossToLogIn(t *testing.T) {
	tr, gh := build(t, "", reply{prefix: []string{"auth", "status"}, stderr: "You are not logged into any GitHub hosts."})
	err := tr.Prepare(context.Background(), []crew.State{crew.Ready})
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("Prepare = %v, want an error telling the boss to run gh auth login", err)
	}
	if calls := gh.callsTo("label"); len(calls) != 0 {
		t.Errorf("touched labels %q without auth", calls)
	}
}

func TestPrepareWithoutGhSaysItIsMissing(t *testing.T) {
	tr, _ := build(t, "", reply{prefix: []string{"auth", "status"}, err: fmt.Errorf("start gh: %w", &exec.Error{Name: "gh", Err: exec.ErrNotFound})})
	err := tr.Prepare(context.Background(), []crew.State{crew.Ready})
	if err == nil || !strings.Contains(err.Error(), "gh") || strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("Prepare = %v, want an error saying gh is not installed", err)
	}
}

func TestPrepareCreatesOnlyTheMissingLabels(t *testing.T) {
	tr, gh := build(t, "",
		reply{prefix: []string{"auth", "status"}},
		reply{prefix: []string{"label", "list"}, stdout: `[{"name":"ready"},{"name":"In Progress"},{"name":"ready to review"},{"name":"bug"}]`},
		reply{prefix: []string{"label", "create"}},
	)
	states := []crew.State{crew.Ready, crew.InProgress, crew.ReadyToReview, crew.InReview, crew.NeedsAttention}
	if err := tr.Prepare(context.Background(), states); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var created []string
	for _, c := range gh.callsTo("label", "create") {
		created = append(created, c[2])
	}
	if want := []string{"in review", "needs attention"}; !slices.Equal(created, want) {
		t.Errorf("created labels %q, want %q", created, want)
	}
}
