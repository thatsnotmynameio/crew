package mates

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// scriptedGh returns a proc.Runner that answers every command with stdout
// and err, and the commands it ran.
func scriptedGh(stdout string, err error) (proc.Runner, *[]proc.Command) {
	var calls []proc.Command
	return func(_ context.Context, c proc.Command) (proc.Output, error) {
		calls = append(calls, c)
		return proc.Output{Stdout: []byte(stdout)}, err
	}, &calls
}

func TestResolveRepoAsksGhFromTheRepositoryRoot(t *testing.T) {
	for name, tc := range map[string]struct {
		ownerType string
		want      Repo
	}{
		"AE1 organization": {"Organization", Repo{Name: "crew", Owner: "thatsnotmynameio", OwnerID: 42, Org: true}},
		"AE1 user":         {"User", Repo{Name: "crew", Owner: "thatsnotmynameio", OwnerID: 42}},
	} {
		t.Run(name, func(t *testing.T) {
			run, calls := scriptedGh(fmt.Sprintf(
				`{"name":"crew","full_name":"thatsnotmynameio/crew","owner":{"login":"thatsnotmynameio","id":42,"type":%q}}`,
				tc.ownerType), nil)
			got, err := ResolveRepo(context.Background(), run, "/repo")
			if err != nil {
				t.Fatalf("ResolveRepo: %v", err)
			}
			if got != tc.want {
				t.Errorf("ResolveRepo = %+v, want %+v", got, tc.want)
			}
			want := proc.Command{Name: "gh", Args: []string{"api", "repos/{owner}/{repo}"}, Dir: "/repo"}
			if len(*calls) != 1 || (*calls)[0].Name != want.Name || (*calls)[0].Dir != want.Dir ||
				!slices.Equal((*calls)[0].Args, want.Args) {
				t.Errorf("ran %+v, want only %+v", *calls, want)
			}
		})
	}
}

func TestResolveRepoFailuresAreEnvironmentErrors(t *testing.T) {
	notFound := fmt.Errorf("start gh: %w", &exec.Error{Name: "gh", Err: exec.ErrNotFound})
	for name, tc := range map[string]struct {
		stdout string
		err    error
		want   []string
	}{
		"gh missing": {err: notFound, want: []string{"not on PATH"}},
		"not logged in": {err: errors.New("gh: exit status 1: To get started with GitHub CLI, please run:  gh auth login"),
			want: []string{"gh auth login", "remote"}},
		"no git remotes": {err: errors.New("gh: exit status 1: no git remotes found"),
			want: []string{"gh auth login", "remote", "no git remotes found"}},
		"unreadable output": {stdout: "not json", want: []string{"unreadable"}},
	} {
		t.Run(name, func(t *testing.T) {
			run, _ := scriptedGh(tc.stdout, tc.err)
			_, err := ResolveRepo(context.Background(), run, "/repo")
			if _, ok := errors.AsType[*EnvError](err); !ok {
				t.Fatalf("ResolveRepo = %v, want an *EnvError", err)
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("ResolveRepo = %v, want it to say %q", err, w)
				}
			}
		})
	}
}
