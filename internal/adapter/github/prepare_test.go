package github

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

func TestPrepareWithoutAuthTellsTheBossToLogIn(t *testing.T) {
	tr, gh := build(t,
		reply{prefix: []string{"auth", "status"}, stderr: "You are not logged into any GitHub hosts."})
	err := tr.Prepare(context.Background(), []crew.State{ready})
	if err == nil || !strings.Contains(err.Error(), "gh auth login") {
		t.Errorf("Prepare = %v, want an error telling the boss to run gh auth login", err)
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
		"another case": {`[{"name":"ready"},{"name":"In Progress"},{"name":"bug"},{"name":"Waiting Brainstorm"}]`,
			[]string{"in review", "needs attention"}},
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t,
				reply{prefix: []string{"auth", "status"}},
				login, noCodeowners,
				reply{prefix: []string{"label", "list"}, stdout: tc.present},
				reply{prefix: []string{"label", "create"}},
			)
			states := []crew.State{ready, inProgress, inReview, needsAttention}
			if err := tr.Prepare(context.Background(), states); err != nil {
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
		})
	}
}
