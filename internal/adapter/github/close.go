package github

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// stateMerged is the state GitHub gives a merged pull request.
const stateMerged = "MERGED"

// unresolved is what gh prints when GitHub has no issue or pull request of
// the number asked for.
const unresolved = "Could not resolve to an issue or pull request"

// Close implements port.Closer, safe to retry whichever step failed. It
// reads the issue's or pull request's state and labels, as you, first: a
// merged pull request cannot be closed, and is refused; one GitHub cannot
// resolve, an open one not in from, and a closed one in crew states but not
// from moved meanwhile. An open one in from is closed through the REST API
// as the writer; a closed one is not closed again. Then, as the writer, it
// takes crew's labels off the open pull requests in the repository that
// close the issue, and off the issue last, so a retry after a failed pull
// request edit still finds them. Each edit only removes labels, leaving
// those that are not crew's, and none runs when there is nothing to remove.
func (t *Tracker) Close(ctx context.Context, id crew.IssueID, from crew.State) error {
	what := fmt.Sprintf("close issue #%s from %s", id.Key, from)
	state, labels, err := t.view(ctx, id.Key)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	names, states := t.crewLabels(labels)
	switch open := state == stateOpen; {
	case state == stateMerged:
		return fmt.Errorf("%s: it is a merged pull request: %w", what, port.ErrRefused)
	case !slices.Contains(states, from) && (open || len(states) > 0):
		return fmt.Errorf("%s: it is no longer %s: %w", what, from, port.ErrMovedMeanwhile)
	case open:
		out, _, err := t.gh.write(ctx, "api", "--method", "PATCH", "repos/{owner}/{repo}/issues/"+id.Key,
			"-f", "state=closed")
		if err != nil {
			return fmt.Errorf("%s: %w", what, classify(err, out, true))
		}
	}
	_, prs, err := t.pullRequests(ctx, id.Key)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	for _, pr := range prs {
		prNames, _ := t.crewLabels(pr.labels)
		if err := t.removeLabels(ctx, "pr", strconv.Itoa(pr.number), prNames); err != nil {
			return fmt.Errorf("%s: edit pull request #%d: %w", what, pr.number, err)
		}
	}
	if err := t.removeLabels(ctx, "issue", id.Key, names); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

// view reads the state and labels of the issue or pull request number, as
// you. A number GitHub cannot resolve is port.ErrMovedMeanwhile.
func (t *Tracker) view(ctx context.Context, number string) (string, []ghLabel, error) {
	out, err := t.gh.call(ctx, "issue", "view", number, "--json", "state,labels")
	if err != nil {
		if strings.Contains(string(out.Stderr), unresolved) {
			return "", nil, fmt.Errorf("%w: %w", port.ErrMovedMeanwhile, err)
		}
		return "", nil, err
	}
	var item struct {
		State  string    `json:"state"`
		Labels []ghLabel `json:"labels"`
	}
	if err := json.Unmarshal(out.Stdout, &item); err != nil {
		return "", nil, fmt.Errorf("gh issue view: unreadable output: %w", err)
	}
	return item.State, item.Labels, nil
}

// crewLabels returns the names of the crew labels among labels, in label
// order, and the states they name, each once.
func (t *Tracker) crewLabels(labels []ghLabel) ([]string, []crew.State) {
	var names []string
	var states []crew.State
	for _, l := range labels {
		s, ok := t.labels.stateOf(l.Name)
		if !ok {
			continue
		}
		names = append(names, l.Name)
		if !slices.Contains(states, s) {
			states = append(states, s)
		}
	}
	return names, states
}

// removeLabels runs one gh <kind> edit of number, an issue's or a pull
// request's, that removes the labels named in names and adds none, and runs
// nothing when names is empty. gh saying one of them does not exist is a
// refusal, as in editLabels.
func (t *Tracker) removeLabels(ctx context.Context, kind, number string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	args := []string{kind, "edit", number}
	for _, name := range names {
		args = append(args, "--remove-label="+labelArg(name))
	}
	if out, _, err := t.gh.write(ctx, args...); err != nil {
		if slices.ContainsFunc(names, func(name string) bool { return missingLabel(string(out.Stderr), name) }) {
			return fmt.Errorf("%w: %w", port.ErrRefused, err)
		}
		return err
	}
	return nil
}
