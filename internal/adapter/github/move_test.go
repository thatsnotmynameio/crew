package github

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// GitHub may apply a combined edit's removal even when its add fails.
// Keeping the source label makes the failed move safe to retry.
func TestMoveKeepsTheSourceLabelWhenTheAddFails(t *testing.T) {
	view := reply{prefix: []string{"issue", "view", "3"},
		stdout: viewJSON("OPEN", ready, "bug")}
	gh := newFakeGh(t, view,
		reply{prefix: []string{"issue", "edit", "3"}, once: true,
			stderr: "Something went wrong while executing your query"},
		reply{prefix: []string{"issue", "edit", "3"}},
	)
	tr, err := factory(func(ctx context.Context, c proc.Command) (proc.Output, error) {
		out, err := gh.run(ctx, c)
		if slices.Contains(c.Args, "--remove-label=ready") {
			// The removal lands even when gh reports a failed addition.
			gh.script[0].stdout = viewJSON("OPEN", "bug")
		}
		return out, err
	})(section(t, ""))
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	err = tr.Move(context.Background(), issueID("3"), ready, inProgress)
	if err == nil || errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused) {
		t.Fatalf("Move = %v, want a transient error", err)
	}
	if gh.script[0].stdout != view.stdout {
		t.Error("the failed add removed the source label")
	}
	if err := tr.Move(context.Background(), issueID("3"), ready, inProgress); err != nil {
		t.Errorf("retry = %v, want the move to finish", err)
	}
}

func TestMoveRetriesOnlyTheRemovalAfterTheAddLands(t *testing.T) {
	tr, gh := build(t,
		reply{prefix: []string{"issue", "view", "3"}, once: true,
			stdout: viewJSON("OPEN", ready, "bug")},
		reply{prefix: []string{"issue", "view", "3"},
			stdout: viewJSON("OPEN", ready, "In Progress", "bug")},
		reply{prefix: []string{"issue", "edit", "3", "--add-label=in progress"}},
		reply{prefix: []string{"issue", "edit", "3", "--remove-label=ready"}, once: true,
			stderr: "Something went wrong while executing your query"},
		reply{prefix: []string{"issue", "edit", "3", "--remove-label=ready"}},
	)
	err := tr.Move(context.Background(), issueID("3"), ready, inProgress)
	if err == nil || errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused) {
		t.Fatalf("Move = %v, want a transient error", err)
	}
	if err := tr.Move(context.Background(), issueID("3"), ready, inProgress); err != nil {
		t.Fatalf("retry: %v", err)
	}
	want := [][]string{
		{"issue", "edit", "3", "--add-label=in progress"},
		{"issue", "edit", "3", "--remove-label=ready"},
		{"issue", "edit", "3", "--remove-label=ready"},
	}
	if edits := gh.callsTo("issue", "edit"); !slices.EqualFunc(edits, want, slices.Equal) {
		t.Errorf("edits = %q, want one add and a retried removal: %q", edits, want)
	}
}

// An error from gh does not prove the mutation failed on GitHub.
func TestMoveRetriesAfterAnEditLandsDespiteItsError(t *testing.T) {
	for name, tc := range map[string]struct {
		failed []string
		labels []crew.State
		want   [][]string
	}{
		"add landed": {
			failed: []string{"issue", "edit", "3", "--add-label=in progress"},
			labels: []crew.State{ready, inProgress},
			want: [][]string{
				{"issue", "edit", "3", "--add-label=in progress"},
				{"issue", "edit", "3", "--remove-label=ready"},
			},
		},
		"removal landed": {
			failed: []string{"issue", "edit", "3", "--remove-label=ready"},
			labels: []crew.State{inProgress},
			want: [][]string{
				{"issue", "edit", "3", "--add-label=in progress"},
				{"issue", "edit", "3", "--remove-label=ready"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := build(t,
				reply{prefix: []string{"issue", "view", "3"}, once: true, stdout: viewJSON("OPEN", ready)},
				reply{prefix: []string{"issue", "view", "3"}, stdout: viewJSON("OPEN", tc.labels...)},
				reply{prefix: tc.failed, once: true, stderr: "Something went wrong while executing your query"},
				reply{prefix: []string{"issue", "edit", "3"}},
			)
			err := tr.Move(context.Background(), issueID("3"), ready, inProgress)
			if err == nil || errors.Is(err, port.ErrMovedMeanwhile) || errors.Is(err, port.ErrRefused) {
				t.Fatalf("Move = %v, want a transient error", err)
			}
			if err := tr.Move(context.Background(), issueID("3"), ready, inProgress); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if edits := gh.callsTo("issue", "edit"); !slices.EqualFunc(edits, tc.want, slices.Equal) {
				t.Errorf("edits = %q, want the landed edit not repeated: %q", edits, tc.want)
			}
		})
	}
}
