package github

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"maps"
	"os"
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
	t.Helper()
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

// build builds the tracker from a config with an empty tracker section, with
// gh scripted.
func build(t *testing.T, script ...reply) (*Tracker, *fakeGh) {
	t.Helper()
	gh := newFakeGh(t, script...)
	tr, err := factory(gh.run)(section(t, ""))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	built, ok := tr.(*Tracker)
	if !ok {
		t.Fatalf("factory built %T, want *Tracker", tr)
	}
	return built, gh
}

// wantClassified checks that err wraps exactly the port errors want wraps:
// port.ErrMovedMeanwhile, port.ErrRefused, or neither when want is nil, a
// transient error.
func wantClassified(t *testing.T, err, want error) {
	t.Helper()
	for _, sentinel := range []error{port.ErrMovedMeanwhile, port.ErrRefused} {
		if got, want := errors.Is(err, sentinel), errors.Is(want, sentinel); got != want {
			t.Errorf("errors.Is(%v, %v) = %t, want %t", err, sentinel, got, want)
		}
	}
}

var login = reply{prefix: []string{"api", "user"}, stdout: "me\n"}

func issuesJSON(nodes ...string) string {
	return `{"data":{"repository":{"issues":{"nodes":[` + strings.Join(nodes, ",") + `]}}}}`
}

func issueNode(number int, created string, labels ...string) string {
	return fmt.Sprintf(`{"number":%d,"title":"Issue %d","url":"https://github.com/o/r/issues/%d",`+
		`"createdAt":%q,"labels":{"nodes":[%s]}}`,
		number, number, number, created, labelNodes(labels...))
}

// labelNodes returns the label nodes of a listed item, as GitHub returns them.
func labelNodes(labels ...string) string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = fmt.Sprintf(`{"name":%q}`, l)
	}
	return strings.Join(names, ",")
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
	tr, gh := build(t, login, reply{
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
	wantItems(t, got, want)
}

func TestListResolvesTheLoginOnce(t *testing.T) {
	tr, gh := build(t, login, reply{prefix: []string{"api", "graphql"}, stdout: issuesJSON()})
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
			tr, _ := build(t, login, reply{
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

func TestListMarksAnIssueBlockedOnlyWhileAnOpenIssueBlocksIt(t *testing.T) {
	// blockedBy counts the open issues blocking it; totalBlockedBy counts
	// the closed ones too.
	tr, gh := build(t, login, reply{
		prefix: []string{"api", "graphql"},
		stdout: issuesJSON(
			blockedNode(issueNode(4, "2026-09-01T10:00:00Z", "ready"), 1, 2),
			blockedNode(issueNode(5, "2026-09-02T10:00:00Z", "ready"), 0, 1),
			issueNode(6, "2026-09-03T10:00:00Z", "ready"),
		),
	})
	got, err := tr.List(context.Background(), []crew.State{ready})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	query := strings.Join(fieldValues(gh.callsTo("api", "graphql")[0], "query"), "")
	if !strings.Contains(query, "issueDependenciesSummary { blockedBy }") {
		t.Errorf("query does not ask for the open issues blocking each issue:\n%s", query)
	}
	blocked := map[string]bool{}
	for _, issue := range got {
		blocked[issue.Key] = issue.Blocked
	}
	if want := map[string]bool{"4": true, "5": false, "6": false}; !maps.Equal(blocked, want) {
		t.Errorf("blocked = %v, want %v", blocked, want)
	}
}

// priorityOptions are the option ids of thatsnotmynameio's issue field
// Priority, in its order: Urgent, High, Medium, Low.
var priorityOptions = []string{"IFSSO_kgDOBGSEBw", "IFSSO_kgDOBGSECA", "IFSSO_kgDOBGSECQ", "IFSSO_kgDOBGSECg"}

// selectValue is one single select value node as GitHub returns it for an
// issue's issueFieldValues, shaped like #14's Priority: the option it holds
// and the field with its options.
func selectValue(field, optionID string, options ...string) string {
	ids := make([]string, len(options))
	for i, o := range options {
		ids[i] = fmt.Sprintf(`{"id":%q}`, o)
	}
	return fmt.Sprintf(`{"optionId":%q,"field":{"name":%q,"options":[%s]}}`, optionID, field, strings.Join(ids, ","))
}

// valuesNode adds to an issueNode its issue field value nodes.
func valuesNode(node string, values ...string) string {
	return strings.TrimSuffix(node, "}") +
		fmt.Sprintf(`,"issueFieldValues":{"nodes":[%s]}}`, strings.Join(values, ","))
}

func TestListReadsEachIssuesPriorityFromItsIssueField(t *testing.T) {
	urgent, medium, low := priorityOptions[0], priorityOptions[2], priorityOptions[3]
	unknown := selectValue("Priority", "IFSSO_unknown", priorityOptions...)
	effort := selectValue("Effort", "IFSSO_high", "IFSSO_high", "IFSSO_low")
	tr, gh := build(t, login, reply{
		prefix: []string{"api", "graphql"},
		stdout: issuesJSON(
			valuesNode(issueNode(1, "2026-09-01T10:00:00Z", "ready"), selectValue("Priority", urgent, priorityOptions...)),
			valuesNode(issueNode(2, "2026-09-02T10:00:00Z", "ready"), selectValue("Priority", low, priorityOptions...)),
			// A date value decodes as an empty object next to Priority.
			valuesNode(issueNode(3, "2026-09-03T10:00:00Z", "ready"), `{}`, selectValue("Priority", medium, priorityOptions...)),
			valuesNode(issueNode(4, "2026-09-04T10:00:00Z", "ready"), selectValue("priority", urgent, priorityOptions...)),
			valuesNode(issueNode(5, "2026-09-05T10:00:00Z", "ready"), unknown),
			valuesNode(issueNode(6, "2026-09-06T10:00:00Z", "ready"), effort),
			// AE5: a repository a user owns has no issue fields.
			valuesNode(issueNode(7, "2026-09-07T10:00:00Z", "ready")),
			issueNode(8, "2026-09-08T10:00:00Z", "ready"),
		),
	})
	got, err := tr.List(context.Background(), []crew.State{ready})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	query := strings.Join(fieldValues(gh.callsTo("api", "graphql")[0], "query"), "")
	for _, want := range []string{"issueFieldValues", "optionId", "options { id }"} {
		if !strings.Contains(query, want) {
			t.Errorf("query does not contain %q:\n%s", want, query)
		}
	}
	// totalCount on issueFieldValues fails the whole query on a repository
	// a user owns.
	if strings.Contains(query, "totalCount") {
		t.Errorf("query asks for totalCount:\n%s", query)
	}
	byKey := map[string]int{}
	for _, issue := range got {
		byKey[issue.Key] = issue.Priority
	}
	want := map[string]int{"1": 1, "2": 4, "3": 3, "4": 1, "5": 0, "6": 0, "7": 0, "8": 0}
	if !maps.Equal(byKey, want) {
		t.Errorf("priorities = %v, want %v", byKey, want)
	}
}

// blockedNode adds to an issueNode the dependency summary GitHub returns:
// open is how many open issues block it, total how many issues do.
func blockedNode(node string, open, total int) string {
	return strings.TrimSuffix(node, "}") +
		fmt.Sprintf(`,"issueDependenciesSummary":{"blockedBy":%d,"totalBlockedBy":%d}}`, open, total)
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
			tr, gh := build(t,
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
		"closed":                `{"state":"CLOSED","labels":[{"name":"ready"}]}`,
		"no longer ready":       `{"state":"OPEN","labels":[{"name":"needs attention"}]}`,
		"in no state":           `{"state":"OPEN","labels":[{"name":"bug"}]}`,
		"in to and other":       `{"state":"OPEN","labels":[{"name":"in progress"},{"name":"in review"}]}`,
		"closed in to":          `{"state":"CLOSED","labels":[{"name":"in progress"}]}`,
		"a merged pull request": `{"state":"MERGED","labels":[{"name":"ready"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t, reply{prefix: []string{"issue", "view", "3"}, stdout: view})
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
			tr, gh := build(t,
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
	tr, _ := build(t,
		reply{prefix: []string{"issue", "view", "3"}, stdout: `{"state":"OPEN","labels":[{"name":"ready"}]}`},
		reply{prefix: []string{"issue", "edit", "3"}, stderr: "could not add label: 'in progress' not found\n"},
	)
	err := tr.Move(context.Background(), "3", ready, inProgress)
	if !errors.Is(err, port.ErrRefused) {
		t.Errorf("Move = %v, want ErrRefused", err)
	}
}

func TestAFailingGhCallIsTransientAndCarriesItsStderr(t *testing.T) {
	tr, _ := build(t,
		reply{prefix: []string{"issue", "view", "3"}, stderr: "HTTP 502: Bad Gateway"},
		reply{prefix: []string{"api", "--method", "POST", "repos/{owner}/{repo}/issues/3/comments"},
			stderr: "HTTP 502: Bad Gateway"},
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
