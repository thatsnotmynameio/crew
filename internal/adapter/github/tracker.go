// Package github is the tracker adapter for GitHub issues, through the gh
// CLI. It maps crew's eight states to labels (tracker.labels), lists the open
// issues the authenticated gh user opened, moves them by swapping labels and
// reports failures as Markdown comments. It works on the repository gh
// resolves from crew's working directory, and runs every gh call through the
// shared process helper.
package github

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Compile-time guards: the tracker is a port.Tracker and a port.Preparer.
var (
	_ port.Tracker  = (*Tracker)(nil)
	_ port.Preparer = (*Tracker)(nil)
)

// issuesQuery lists the login's open issues carrying any of the labels,
// oldest first. GitHub's labels filter matches an issue with any of them.
const issuesQuery = `query($owner: String!, $name: String!, $login: String!, $labels: [String!]) {
  repository(owner: $owner, name: $name) {
    issues(first: 100, states: OPEN, filterBy: {createdBy: $login, labels: $labels},
           orderBy: {field: CREATED_AT, direction: ASC}) {
      nodes {
        number
        title
        url
        createdAt
        labels(first: 100) { nodes { name } }
      }
    }
  }
}`

// ghLabel is a label as gh prints it in JSON.
type ghLabel struct {
	Name string `json:"name"`
}

// Tracker is the GitHub tracker. It is safe for concurrent use.
type Tracker struct {
	gh     *gh
	labels labels
}

// Factory returns the github tracker's factory, which runs gh through group.
// The factory decodes tracker.labels: each key must be a crew state, a state
// left out maps to its key with "_" replaced by a space, and two states
// mapping to the same name, compared case-insensitively, are an error naming
// both. It runs no gh call; Prepare does.
func Factory(group *proc.Group) port.TrackerFactory {
	return factory(group.Run)
}

func factory(run proc.Runner) port.TrackerFactory {
	return func(decode port.Decode) (port.Tracker, error) {
		l, err := decodeLabels(decode)
		if err != nil {
			return nil, err
		}
		return &Tracker{gh: &gh{run: run}, labels: l}, nil
	}
}

// List implements port.Tracker with one GraphQL query: the open issues the
// authenticated gh user opened that carry any of the states' labels, oldest
// first, at most 100. Each issue's key is its number, its reference
// #<number>, and its states every crew state its labels map to.
func (t *Tracker) List(ctx context.Context, states []crew.State) ([]crew.Issue, error) {
	login, err := t.gh.viewer(ctx)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	args := []string{"api", "graphql",
		"-f", "query=" + issuesQuery,
		// gh fills {owner} and {repo} from the repository, through -F only.
		"-F", "owner={owner}", "-F", "name={repo}",
		"-f", "login=" + login}
	for _, s := range states {
		args = append(args, "-f", "labels[]="+t.labels.name[s])
	}
	var reply struct {
		Data struct {
			Repository struct {
				Issues struct {
					Nodes []struct {
						Number    int       `json:"number"`
						Title     string    `json:"title"`
						URL       string    `json:"url"`
						CreatedAt time.Time `json:"createdAt"`
						Labels    struct {
							Nodes []ghLabel `json:"nodes"`
						} `json:"labels"`
					} `json:"nodes"`
				} `json:"issues"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := t.gh.decode(ctx, &reply, args...); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	nodes := reply.Data.Repository.Issues.Nodes
	issues := make([]crew.Issue, 0, len(nodes))
	for _, n := range nodes {
		key := strconv.Itoa(n.Number)
		issue := crew.Issue{Key: key, Ref: "#" + key, Title: n.Title, URL: n.URL, Created: n.CreatedAt}
		for _, l := range n.Labels.Nodes {
			if s, ok := t.labels.stateOf(l.Name); ok && !slices.Contains(issue.States, s) {
				issue.States = append(issue.States, s)
			}
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

// Move implements port.Tracker. It reads the issue's state and labels; a
// closed issue, or one without from's label, moved meanwhile. Otherwise one
// gh issue edit removes every other crew label the issue carries and adds
// to's, leaving its other labels alone. gh saying a label does not exist is a
// refusal: the label must be created, which retrying cannot do.
func (t *Tracker) Move(ctx context.Context, issueKey string, from, to crew.State) error {
	var issue struct {
		State  string    `json:"state"`
		Labels []ghLabel `json:"labels"`
	}
	move := fmt.Sprintf("move issue #%s from %s to %s", issueKey, from, to)
	if err := t.gh.decode(ctx, &issue, "issue", "view", issueKey, "--json", "state,labels"); err != nil {
		return fmt.Errorf("%s: %w", move, err)
	}
	if issue.State != "OPEN" {
		return fmt.Errorf("%s: it is %s: %w", move, strings.ToLower(issue.State), port.ErrMovedMeanwhile)
	}
	args := []string{"issue", "edit", issueKey}
	inFrom := false
	for _, l := range issue.Labels {
		s, ok := t.labels.stateOf(l.Name)
		inFrom = inFrom || (ok && s == from)
		if ok && s != to {
			args = append(args, "--remove-label="+l.Name)
		}
	}
	if !inFrom {
		return fmt.Errorf("%s: it is no longer %s: %w", move, from, port.ErrMovedMeanwhile)
	}
	target := t.labels.name[to]
	args = append(args, "--add-label="+target)
	if out, err := t.gh.call(ctx, args...); err != nil {
		if missingLabel(string(out.Stderr), target) {
			return fmt.Errorf("%s: %w: %w", move, port.ErrRefused, err)
		}
		return fmt.Errorf("%s: %w", move, err)
	}
	return nil
}

// missingLabel reports whether gh's stderr says label does not exist, in
// either of the forms gh prints.
func missingLabel(stderr, label string) bool {
	stderr = strings.ToLower(stderr)
	return strings.Contains(stderr, "'"+strings.ToLower(label)+"' not found") ||
		strings.Contains(stderr, "labels not found")
}

// ReportFailure implements port.Tracker: one Markdown comment naming each
// failed action, its workspace and its log, with each reason in a fenced
// code block. Its errors are transient.
func (t *Tracker) ReportFailure(ctx context.Context, report crew.FailureReport) error {
	if _, err := t.gh.call(ctx, "issue", "comment", report.IssueKey, "--body="+renderReport(report)); err != nil {
		return fmt.Errorf("report failure on issue #%s: %w", report.IssueKey, err)
	}
	return nil
}

// Prepare implements port.Preparer. It checks that gh is installed and
// logged in, then creates the labels of states the repository lacks,
// comparing names case-insensitively, and only those.
func (t *Tracker) Prepare(ctx context.Context, states []crew.State) error {
	if _, err := t.gh.call(ctx, "auth", "status"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("tracker github needs the gh CLI, which is not on PATH: %w", err)
		}
		return fmt.Errorf("tracker github: gh is not logged in to GitHub; run `gh auth login`: %w", err)
	}
	var present []ghLabel
	if err := t.gh.decode(ctx, &present, "label", "list", "--limit", "1000", "--json", "name"); err != nil {
		return fmt.Errorf("tracker github: read the repository's labels: %w", err)
	}
	have := make(map[string]bool, len(present))
	for _, l := range present {
		have[strings.ToLower(l.Name)] = true
	}
	for _, s := range states {
		name := t.labels.name[s]
		if have[strings.ToLower(name)] {
			continue
		}
		if _, err := t.gh.call(ctx, "label", "create", name); err != nil {
			return fmt.Errorf("tracker github: create the label %q for state %s: %w", name, s, err)
		}
		have[strings.ToLower(name)] = true
	}
	return nil
}
