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
	steps = append(steps, "checking the gh login", "finding the boss", "reading the repository's labels")
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
		t.Errorf("Prepare = %v, want an error telling the boss to run gh auth login", err)
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

func TestPrepareCreatesOnlyTheMissingLabels(t *testing.T) {
	for name, tc := range map[string]struct {
		present string
		want    []string
	}{
		"AE7 only ready": {`[{"name":"ready"}]`,
			[]string{"in progress", "in review", "needs attention", "waiting brainstorm"}},
		"AE1 another case": {`[{"name":"ready"},{"name":"In Progress"},{"name":"bug"},{"name":"Waiting Brainstorm"}]`,
			[]string{"in review", "needs attention"}},
		"AE2 every label": {`[{"name":"Ready"},{"name":"in progress"},{"name":"IN REVIEW"},` +
			`{"name":"needs attention"},{"name":"waiting brainstorm"}]`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t,
				reply{prefix: []string{"auth", "status"}},
				login, noCodeowners,
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
