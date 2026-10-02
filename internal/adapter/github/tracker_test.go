package github

import (
	"context"
	"encoding/csv"
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

// The workflow's states, as label text.
const (
	ready          crew.State = "ready"
	inProgress     crew.State = "in progress"
	readyToReview  crew.State = "ready to review"
	inReview       crew.State = "in review"
	needsAttention crew.State = "needs attention"
)

// extraLabels is the config's one extra label, waiting brainstorm: parked
// work no stage takes.
const extraLabels = `extra_labels:
  - label: waiting brainstorm
`

// workflow is the draft config's workflow, so the files below load. Its
// states are ready, in progress, ready to review, needs attention, in review
// and ready to merge; it does not name paused.
const workflow = `workflow:
  - name: implement
    label: ready
    moves_to: in progress
    on_success: ready to review
    on_failure: needs attention
    actions:
      - name: development
        prompt: "Implement {{.Issue.Ref}}"
  - name: review
    label: ready to review
    moves_to: in review
    on_success: ready to merge
    on_failure: needs attention
    actions:
      - name: custom_review
        prompt: "Review {{.Issue.Ref}}"
`

// section loads a .crew/config.yaml holding tracker, which is the tracker:
// section's body, and returns the section's strict decoder, the workflow's
// states and the extra labels, as the app passes them to the factory.
func section(t *testing.T, tracker string) (port.Decode, []crew.State, []crew.State) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".crew"), 0o750); err != nil {
		t.Fatal(err)
	}
	body := "tracker:\n  name: github\n" + tracker + extraLabels + workflow
	if err := os.WriteFile(filepath.Join(root, ".crew", "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg.TrackerSection, crew.WorkflowStates(cfg.Workflow), cfg.Extras
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

	got, err := tr.List(context.Background(), []crew.State{ready, readyToReview})
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
			Created: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC), States: []crew.State{ready}},
		{Key: "14", Ref: "#14", Title: "Issue 14", URL: "https://github.com/o/r/issues/14",
			Created: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC), States: []crew.State{readyToReview}},
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
		if _, err := tr.List(context.Background(), []crew.State{ready}); err != nil {
			t.Fatalf("List: %v", err)
		}
	}
	if n := len(gh.callsTo("api", "user")); n != 1 {
		t.Errorf("asked gh for the login %d times, want 1", n)
	}
}

func TestListReturnsEveryCrewStateOfAnIssueInTheWorkflowsSpelling(t *testing.T) {
	for name, tc := range map[string]struct {
		labels []string
		want   []crew.State
	}{
		"two crew labels":            {[]string{"ready", "Needs Attention", "bug"}, []crew.State{ready, needsAttention}},
		"another case":               {[]string{"Ready"}, []crew.State{ready}},
		"AE6 a label no stage names": {[]string{"paused", "ready"}, []crew.State{ready}},
		"an extra label":             {[]string{"waiting brainstorm", "ready"}, []crew.State{ready}},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _ := build(t, "", login, reply{
				prefix: []string{"api", "graphql"},
				stdout: issuesJSON(issueNode(4, "2026-09-01T10:00:00Z", tc.labels...)),
			})
			got, err := tr.List(context.Background(), []crew.State{ready})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(got) != 1 || !slices.Equal(got[0].States, tc.want) {
				t.Errorf("List = %+v, want #4 in %q", got, tc.want)
			}
		})
	}
}

// Covers AE1, AE4 and AE6: the move swaps crew's labels, removes every extra
// label, and leaves the others, bug and paused, which no stage names.
func TestMoveSwapsTheCrewLabelsInOneEdit(t *testing.T) {
	for name, tc := range map[string]struct {
		labels string
		want   []string
	}{
		"AE1 one crew label": {`{"name":"ready"},{"name":"bug"}`,
			[]string{"--remove-label=ready", "--add-label=in progress"}},
		"two crew labels": {`{"name":"ready"},{"name":"bug"},{"name":"Needs Attention"}`,
			[]string{"--remove-label=ready", "--remove-label=Needs Attention", "--add-label=in progress"}},
		"AE6 a label no stage names": {`{"name":"paused"},{"name":"ready"}`,
			[]string{"--remove-label=ready", "--add-label=in progress"}},
		"AE4 an extra label": {`{"name":"waiting brainstorm"},{"name":"ready"},{"name":"bug"}`,
			[]string{"--remove-label=waiting brainstorm", "--remove-label=ready", "--add-label=in progress"}},
		"an extra label in another case": {`{"name":"ready"},{"name":"Waiting Brainstorm"}`,
			[]string{"--remove-label=ready", "--remove-label=Waiting Brainstorm", "--add-label=in progress"}},
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t, "",
				reply{prefix: []string{"issue", "view", "3"}, stdout: `{"state":"OPEN","labels":[` + tc.labels + `]}`},
				reply{prefix: []string{"issue", "edit", "3"}},
			)
			if err := tr.Move(context.Background(), "3", ready, inProgress); err != nil {
				t.Fatalf("Move: %v", err)
			}
			edits := gh.callsTo("issue", "edit")
			want := append([]string{"issue", "edit", "3"}, tc.want...)
			if len(edits) != 1 || !slices.Equal(edits[0], want) {
				t.Errorf("edits = %q, want one: %q", edits, want)
			}
		})
	}
}

func TestMoveOfAnIssueThatMovedMeanwhileEditsNothing(t *testing.T) {
	for name, view := range map[string]string{
		"closed":          `{"state":"CLOSED","labels":[{"name":"ready"}]}`,
		"no longer ready": `{"state":"OPEN","labels":[{"name":"needs attention"}]}`,
		"in no state":     `{"state":"OPEN","labels":[{"name":"bug"}]}`,
		"in to and other": `{"state":"OPEN","labels":[{"name":"in progress"},{"name":"in review"}]}`,
		"closed in to":    `{"state":"CLOSED","labels":[{"name":"in progress"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t, "", reply{prefix: []string{"issue", "view", "3"}, stdout: view})
			err := tr.Move(context.Background(), "3", ready, inProgress)
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
// already in to: the move is done, and nothing is edited again (KTD8). An
// extra label added since does not make it a move made meanwhile.
func TestMoveOfAnIssueAlreadyInToIsDoneWithoutAnEdit(t *testing.T) {
	for name, labels := range map[string]string{
		"in to":                           `{"name":"In Progress"},{"name":"bug"}`,
		"in to with an extra added since": `{"name":"in progress"},{"name":"waiting brainstorm"}`,
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t, "",
				reply{prefix: []string{"issue", "view", "3"}, stdout: `{"state":"OPEN","labels":[` + labels + `]}`},
			)
			if err := tr.Move(context.Background(), "3", ready, inProgress); err != nil {
				t.Fatalf("Move = %v, want nil", err)
			}
			if edits := gh.callsTo("issue", "edit"); len(edits) != 0 {
				t.Errorf("sent edits %q, want none", edits)
			}
		})
	}
}

// gh reads --add-label and --remove-label as comma-separated values, so a
// label holding a comma or a double quote must reach gh as one CSV field.
func TestMovePassesALabelWithACommaOrQuoteAsOneLabel(t *testing.T) {
	blocked := crew.State(`blocked, "waiting"`)
	gh := newFakeGh(t,
		reply{prefix: []string{"issue", "view", "3"}, stdout: `{"state":"OPEN","labels":[{"name":"blocked, \"waiting\""}]}`},
		reply{prefix: []string{"issue", "edit", "3"}},
	)
	tr, err := factory(gh.run)(func(any) error { return nil }, []crew.State{ready, blocked}, nil)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if err := tr.Move(context.Background(), "3", blocked, ready); err != nil {
		t.Fatalf("Move: %v", err)
	}
	edits := gh.callsTo("issue", "edit")
	if len(edits) != 1 {
		t.Fatalf("edits = %q, want one", edits)
	}
	want := map[string]string{"--remove-label=": string(blocked), "--add-label=": string(ready)}
	for _, arg := range edits[0][3:] {
		for flag, label := range want {
			value, ok := strings.CutPrefix(arg, flag)
			if !ok {
				continue
			}
			got, err := csv.NewReader(strings.NewReader(value)).Read()
			if err != nil || !slices.Equal(got, []string{label}) {
				t.Errorf("%s%s reads as %q (%v), want the one label %q", flag, value, got, err, label)
			}
			delete(want, flag)
		}
	}
	if len(want) != 0 {
		t.Errorf("edit %q lacks %v", edits[0], want)
	}
}

func TestMoveToAMissingLabelIsRefused(t *testing.T) {
	tr, _ := build(t, "",
		reply{prefix: []string{"issue", "view", "3"}, stdout: `{"state":"OPEN","labels":[{"name":"ready"}]}`},
		reply{prefix: []string{"issue", "edit", "3"}, stderr: "could not add label: 'in progress' not found\n"},
	)
	err := tr.Move(context.Background(), "3", ready, inProgress)
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
		"Move": tr.Move(context.Background(), "3", ready, inProgress),
		"ReportFailure": tr.ReportFailure(context.Background(), crew.FailureReport{
			IssueKey: "3", IssueRef: "#3", Failures: []crew.ActionFailure{{Action: "development"}},
		}),
	}
	_, errs["List"] = tr.List(context.Background(), []crew.State{ready})
	for name, err := range errs {
		if err == nil || !strings.Contains(err.Error(), "HTTP 502: Bad Gateway") {
			t.Errorf("%s = %v, want an error carrying gh's stderr", name, err)
		}
		if errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused) {
			t.Errorf("%s = %v, want a transient error", name, err)
		}
	}
}

func TestUnknownTrackerKeysFailNamingThem(t *testing.T) {
	for name, tc := range map[string]struct{ tracker, want string }{
		"misspelt key": {"  lables:\n    ready: todo\n", "tracker.lables (line 3): unknown key"},
		// Covers AE3: tracker.labels is no longer a key.
		"labels": {"  labels:\n    ready: ready\n", "tracker.labels (line 3): unknown key"},
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
	err := tr.Prepare(context.Background(), []crew.State{ready})
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("Prepare = %v, want an error telling the boss to run gh auth login", err)
	}
	if calls := gh.callsTo("label"); len(calls) != 0 {
		t.Errorf("touched labels %q without auth", calls)
	}
}

func TestPrepareWithoutGhSaysItIsMissing(t *testing.T) {
	tr, _ := build(t, "", reply{prefix: []string{"auth", "status"}, err: fmt.Errorf("start gh: %w", &exec.Error{Name: "gh", Err: exec.ErrNotFound})})
	err := tr.Prepare(context.Background(), []crew.State{ready})
	if err == nil || !strings.Contains(err.Error(), "gh") || strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("Prepare = %v, want an error saying gh is not installed", err)
	}
}

func TestPrepareCreatesOnlyTheMissingLabels(t *testing.T) {
	for name, tc := range map[string]struct {
		present string
		want    []string
	}{
		"AE7 only ready": {`[{"name":"ready"}]`, []string{"in progress", "in review", "needs attention", "waiting brainstorm"}},
		"another case": {`[{"name":"ready"},{"name":"In Progress"},{"name":"bug"},{"name":"Waiting Brainstorm"}]`,
			[]string{"in review", "needs attention"}},
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t, "",
				reply{prefix: []string{"auth", "status"}},
				reply{prefix: []string{"label", "list"}, stdout: tc.present},
				reply{prefix: []string{"label", "create"}},
			)
			states := []crew.State{ready, inProgress, inReview, needsAttention}
			if err := tr.Prepare(context.Background(), states); err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			var created []string
			for _, c := range gh.callsTo("label", "create") {
				created = append(created, c[2])
			}
			if !slices.Equal(created, tc.want) {
				t.Errorf("created labels %q, want %q", created, tc.want)
			}
		})
	}
}
