package github

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// recordSteps returns a context whose reporter appends each step Prepare
// reports to the returned slice.
func recordSteps() (context.Context, *[]string) {
	var steps []string
	return port.WithSteps(context.Background(), func(step string) { steps = append(steps, step) }), &steps
}

// prepareSteps returns the steps a Prepare that succeeds reports: the gh
// login, the code owners, the labels, then one per label it creates, naming it.
func prepareSteps(created []string) []string {
	steps := make([]string, 0, 3+len(created))
	steps = append(steps, "checking the gh login", "finding the code owners", "reading the repository's labels")
	for _, name := range created {
		steps = append(steps, fmt.Sprintf("creating the label %q", name))
	}
	return steps
}

func TestPrepareWithoutAuthTellsYouToLogIn(t *testing.T) {
	tr, gh := build(t,
		reply{prefix: []string{"auth", "status"}, stderr: "You are not logged into any GitHub hosts."})
	ctx, steps := recordSteps()
	err := tr.Prepare(ctx, []crew.State{ready})
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("Prepare = %v, want an error telling you to run gh auth login", err)
	}
	if want := []string{"checking the gh login"}; !slices.Equal(*steps, want) {
		t.Errorf("steps = %q, want %q", *steps, want)
	}
	if calls := gh.callsTo("label"); len(calls) != 0 {
		t.Errorf("touched labels %q without auth", calls)
	}
}

func TestPrepareWithoutGhSaysItIsMissing(t *testing.T) {
	notFound := fmt.Errorf("start gh: %w", &exec.Error{Name: "gh", Err: exec.ErrNotFound})
	tr, _ := build(t, reply{prefix: []string{"auth", "status"}, err: notFound})
	err := tr.Prepare(context.Background(), []crew.State{ready})
	if err == nil || !strings.Contains(err.Error(), "gh") || strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("Prepare = %v, want an error saying gh is not installed", err)
	}
}

// Prepare creates each rule label the repository lacks, and no other (R24).
func TestPrepareCreatesOnlyTheMissingLabels(t *testing.T) {
	for name, tc := range map[string]struct {
		present string
		want    []string
	}{
		"AE7 only ready": {`[{"name":"ready"}]`,
			[]string{"in progress", "in review", "needs attention"}},
		"AE1 another case": {`[{"name":"ready"},{"name":"In Progress"},{"name":"bug"}]`,
			[]string{"in review", "needs attention"}},
		"AE2 every label": {`[{"name":"Ready"},{"name":"in progress"},{"name":"IN REVIEW"},` +
			`{"name":"needs attention"}]`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t,
				reply{prefix: []string{"auth", "status"}},
				login, noCodeowners, repositoryReply,
				reply{prefix: []string{"label", "list"}, stdout: tc.present},
				reply{prefix: []string{"label", "create"}},
			)
			states := []crew.State{ready, inProgress, inReview, needsAttention}
			ctx, steps := recordSteps()
			if err := tr.Prepare(ctx, states); err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			creates := gh.callsTo("label", "create")
			created := make([]string, 0, len(creates))
			for _, c := range creates {
				created = append(created, c[2])
			}
			if !slices.Equal(created, tc.want) {
				t.Errorf("created labels %q, want %q", created, tc.want)
			}
			if want := prepareSteps(tc.want); !slices.Equal(*steps, want) {
				t.Errorf("steps = %q, want %q", *steps, want)
			}
		})
	}
}

// Prepare reads the repository's node id and name, as you, right after it
// reports reading the labels and before it lists them, with no step of its
// own (R20).
func TestPrepareReadsTheRepository(t *testing.T) {
	tr, gh := build(t,
		reply{prefix: []string{"auth", "status"}}, login, noCodeowners, repositoryReply,
		reply{prefix: []string{"label", "list"}, stdout: `[{"name":"ready"}]`},
	)
	var finder port.RepositoryFinder = tr
	if got := finder.Repository(); got != (crew.Repository{}) {
		t.Errorf("Repository before Prepare = %+v, want the zero value", got)
	}
	ctx, steps := recordSteps()
	if err := tr.Prepare(ctx, []crew.State{ready}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if got := finder.Repository(); got != widgets {
		t.Errorf("Repository = %+v, want %+v", got, widgets)
	}
	want := []string{"api", "graphql", "-f", "query=" + repositoryQuery, "-F", "owner={owner}", "-F", "name={repo}"}
	if got := gh.callsTo("api", "graphql"); len(got) != 1 || !slices.Equal(got[0], want) {
		t.Errorf("graphql calls = %q, want one: %q", got, want)
	}
	read := slices.IndexFunc(gh.calls, func(c []string) bool { return slices.Equal(c, want) })
	list := slices.IndexFunc(gh.calls, func(c []string) bool { return slices.Equal(c[:2], []string{"label", "list"}) })
	if read < 0 || list < 0 || read > list {
		t.Errorf("gh calls %q: want the repository read before the label list", gh.calls)
	}
	if !slices.Equal(*steps, prepareSteps(nil)) {
		t.Errorf("steps = %q, want %q", *steps, prepareSteps(nil))
	}
}

// When the repository read fails, Prepare fails, naming the read, and runs no
// later gh call.
func TestPrepareStopsWhenItCannotReadTheRepository(t *testing.T) {
	for name, tc := range map[string]struct {
		reply reply
		want  string
	}{
		"gh fails": {reply{stderr: "gh: Could not resolve to a Repository with the name 'acme/widgets'."},
			"Could not resolve to a Repository"},
		"unreadable output": {reply{stdout: "not json"}, "unreadable output"},
		"no id":             {reply{stdout: `{"data":{"repository":{"nameWithOwner":"acme/widgets"}}}`}, "no id"},
	} {
		t.Run(name, func(t *testing.T) {
			failing := tc.reply
			failing.prefix = repositoryReply.prefix
			tr, gh := build(t, reply{prefix: []string{"auth", "status"}}, login, noCodeowners, failing)
			ctx, steps := recordSteps()
			err := tr.Prepare(ctx, []crew.State{ready})
			if err == nil || !strings.HasPrefix(err.Error(), "tracker github: read the repository: ") ||
				!strings.Contains(err.Error(), tc.want) {
				t.Errorf("Prepare = %v, want an error reading the repository, saying %q", err, tc.want)
			}
			if calls := gh.callsTo("label"); len(calls) != 0 {
				t.Errorf("ran %q after the repository read failed", calls)
			}
			if got := tr.Repository(); got != (crew.Repository{}) {
				t.Errorf("Repository = %+v, want the zero value", got)
			}
			if want := prepareSteps(nil); !slices.Equal(*steps, want) {
				t.Errorf("steps = %q, want %q", *steps, want)
			}
		})
	}
}
