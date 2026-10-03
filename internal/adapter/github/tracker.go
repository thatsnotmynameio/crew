// Package github is the tracker adapter for GitHub issues, through the gh
// CLI. A workflow state is the label of the same name, compared ignoring
// case as GitHub does, and crew's labels are the workflow's states plus the
// config's extra labels, which are never states. It lists the open issues
// the authenticated gh user opened, moves them by swapping crew's labels,
// reports failures as Markdown comments and keeps a status comment on each
// issue, with one entry per stage run. It puts the open pull requests that
// close an issue in the issue's crew label, and comments on them when a stage
// ends that nobody watches them any more. It works on the repository gh
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
	"sync"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Compile-time guards: the tracker is a port.Tracker, a port.Preparer, a
// port.StatusReporter and a port.PullRequestReporter.
var (
	_ port.Tracker             = (*Tracker)(nil)
	_ port.Preparer            = (*Tracker)(nil)
	_ port.StatusReporter      = (*Tracker)(nil)
	_ port.PullRequestReporter = (*Tracker)(nil)
)

// issuesQuery lists the login's open issues carrying any of the labels,
// oldest first. GitHub's labels filter matches an issue with any of them.
// A dependency summary's blockedBy counts only the open issues blocking it.
// An issue holds at most one value per issue field, and an organization has
// at most 25 fields. A single select value carries its option's id and its
// field, with the field's options in order. The query must not ask for the
// values' totalCount: on a repository a user owns, that fails the query.
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
        issueDependenciesSummary { blockedBy }
        issueFieldValues(first: 25) {
          nodes {
            ... on IssueFieldSingleSelectValue {
              optionId
              field { ... on IssueFieldSingleSelect { name options { id } } }
            }
          }
        }
      }
    }
  }
}`

// issuesReply is issuesQuery's reply.
type issuesReply struct {
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
					Dependencies struct {
						BlockedBy int `json:"blockedBy"`
					} `json:"issueDependenciesSummary"`
					FieldValues struct {
						Nodes []fieldValue `json:"nodes"`
					} `json:"issueFieldValues"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	} `json:"data"`
}

// ghLabel is a label as gh prints it in JSON.
type ghLabel struct {
	Name string `json:"name"`
}

// Tracker is the GitHub tracker. It is safe for concurrent use.
type Tracker struct {
	gh     *gh
	labels labels
	extras extras

	mu       sync.Mutex
	comments map[string]cachedStatus // status comments by issue key, as last written or read
	stopped  map[string][]int        // pull requests given a report's stop comment, by report ID
}

// Factory returns the github tracker's factory, which runs gh through group.
// The tracker section has no key, so the factory refuses any. The tracker
// knows the workflow's states and the extras as its labels. It runs no gh
// call; Prepare does.
func Factory(group *proc.Group) port.TrackerFactory {
	return factory(group.Run)
}

func factory(run proc.Runner) port.TrackerFactory {
	return func(decode port.Decode, states, extraLabels []crew.State) (port.Tracker, error) {
		if err := decode(&settings{}); err != nil {
			return nil, err
		}
		return &Tracker{gh: &gh{run: run}, labels: newLabels(states), extras: slices.Clone(extraLabels),
			comments: map[string]cachedStatus{}, stopped: map[string][]int{}}, nil
	}
}

// List implements port.Tracker with one GraphQL query: the open issues the
// authenticated gh user opened that carry any of the states' labels, oldest
// first, at most 100. Each issue's key is its number, its reference
// #<number>, and its states every workflow state its labels name, in the
// workflow's spelling. Its other labels, extras included, are no states and
// are ignored. It is blocked while an open issue blocks it, as GitHub's
// issue dependencies record. Its priority is the position of its value of
// the issue field Priority among that field's options, the first being 1;
// an issue without one has priority 0.
func (t *Tracker) List(ctx context.Context, states []crew.State) ([]crew.Issue, error) {
	login, err := t.gh.viewer(ctx)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	var reply issuesReply
	if err := t.gh.decode(ctx, &reply, issuesArgs(login, states)...); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	nodes := reply.Data.Repository.Issues.Nodes
	issues := make([]crew.Issue, 0, len(nodes))
	for _, n := range nodes {
		key := strconv.Itoa(n.Number)
		issue := crew.Issue{Key: key, Ref: "#" + key, Title: n.Title, URL: n.URL, Created: n.CreatedAt,
			Blocked: n.Dependencies.BlockedBy > 0, Priority: priority(n.FieldValues.Nodes)}
		for _, l := range n.Labels.Nodes {
			if s, ok := t.labels.stateOf(l.Name); ok && !slices.Contains(issue.States, s) {
				issue.States = append(issue.States, s)
			}
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

// fieldArgs is how many arguments one gh api field takes: the flag and
// key=value.
const fieldArgs = 2

// issuesArgs returns the gh arguments of List's query, for login's issues
// carrying any of the states' labels.
func issuesArgs(login string, states []crew.State) []string {
	labels := make([]string, 0, fieldArgs*len(states))
	for _, s := range states {
		labels = append(labels, "-f", "labels[]="+string(s))
	}
	return slices.Concat([]string{"api", "graphql",
		"-f", "query=" + issuesQuery,
		// gh fills {owner} and {repo} from the repository, through -F only.
		"-F", "owner={owner}", "-F", "name={repo}",
		"-f", "login=" + login}, labels)
}

// priorityField is the name of the issue field crew ranks issues by,
// compared ignoring case.
const priorityField = "Priority"

// fieldValue is an issue field value as issuesQuery reads it. A value of
// another type than single select decodes empty.
type fieldValue struct {
	OptionID string `json:"optionId"`
	Field    struct {
		Name    string `json:"name"`
		Options []struct {
			ID string `json:"id"`
		} `json:"options"`
	} `json:"field"`
}

// priority returns the rank of the Priority value among values: its
// option's position in the field's options, the first being 1. Without a
// Priority value, or with one whose option is not among the field's, it is 0.
func priority(values []fieldValue) int {
	for _, v := range values {
		if !strings.EqualFold(v.Field.Name, priorityField) {
			continue
		}
		for i, o := range v.Field.Options {
			if o.ID == v.OptionID {
				return i + 1
			}
		}
		return 0
	}
	return 0
}

// Move implements port.Tracker. It reads the issue's state and labels; a
// closed issue moved meanwhile. An open issue whose only state label is to's,
// whatever extras it carries, is already moved, as when an earlier attempt
// landed although gh reported an error, so Move returns nil without an edit
// and a retry is safe (KTD8). Any other issue without from's label moved
// meanwhile. Otherwise one gh issue edit removes every other crew label the
// issue carries, extras included, and adds to's, leaving the labels that are
// not crew's alone. gh saying a label does not exist is a refusal: the label
// must be created, which retrying cannot do.
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
	remove, states := t.swap(issue.Labels, to)
	if !slices.Contains(states, from) {
		if slices.Equal(states, []crew.State{to}) {
			return nil
		}
		return fmt.Errorf("%s: it is no longer %s: %w", move, from, port.ErrMovedMeanwhile)
	}
	if err := t.editLabels(ctx, "issue", issueKey, remove, to); err != nil {
		return fmt.Errorf("%s: %w", move, err)
	}
	return nil
}

// ReportFailure implements port.Tracker: one Markdown comment naming each
// failed action and its log, without its reason. The issue gone (HTTP 404
// or 410) is port.ErrMovedMeanwhile, a refusal (HTTP 403, such as a locked
// issue, but not a rate limit) is port.ErrRefused, and any other error is
// transient.
func (t *Tracker) ReportFailure(ctx context.Context, report crew.FailureReport) error {
	if _, err := t.postComment(ctx, report.IssueKey, renderReport(report)); err != nil {
		return fmt.Errorf("report failure on issue #%s: %w", report.IssueKey, err)
	}
	return nil
}

// Prepare implements port.Preparer. It checks that gh is installed and
// logged in, then creates the labels of states and the extras the repository
// lacks, comparing names case-insensitively, and no other label.
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
	for _, s := range slices.Concat(states, []crew.State(t.extras)) {
		name := string(s)
		if have[strings.ToLower(name)] {
			continue
		}
		if _, err := t.gh.call(ctx, "label", "create", name); err != nil {
			return fmt.Errorf("tracker github: create the label %q: %w", name, err)
		}
		have[strings.ToLower(name)] = true
	}
	return nil
}

// editLabels runs one gh <kind> edit of number, an issue's or a pull
// request's, that removes the labels remove names, as swap returns them, and
// adds to's. gh saying to's label does not exist is a refusal: the label must
// be created, which retrying cannot do.
func (t *Tracker) editLabels(ctx context.Context, kind, number string, remove []string, to crew.State) error {
	target := string(to)
	args := slices.Concat([]string{kind, "edit", number}, remove, []string{"--add-label=" + labelArg(target)})
	if out, err := t.gh.call(ctx, args...); err != nil {
		if missingLabel(string(out.Stderr), target) {
			return fmt.Errorf("%w: %w", port.ErrRefused, err)
		}
		return err
	}
	return nil
}

// swap returns the --remove-label arguments that take every crew label but
// to's off a labelable carrying labels, extras included, leaving the labels
// that are not crew's, and the states labels name, each once, in label order.
// Move and the pull request mirror both swap labels through it.
func (t *Tracker) swap(labels []ghLabel, to crew.State) ([]string, []crew.State) {
	var remove []string
	var states []crew.State
	for _, l := range labels {
		if t.extras.has(l.Name) {
			remove = append(remove, "--remove-label="+labelArg(l.Name))
			continue
		}
		s, ok := t.labels.stateOf(l.Name)
		if !ok {
			continue
		}
		if !slices.Contains(states, s) {
			states = append(states, s)
		}
		if s != to {
			remove = append(remove, "--remove-label="+labelArg(l.Name))
		}
	}
	return remove, states
}

// labelArg returns label as one value of gh's --add-label and --remove-label,
// which gh reads as comma-separated values: a label holding a comma or a
// double quote is quoted as one CSV field.
func labelArg(label string) string {
	if !strings.ContainsAny(label, `,"`) {
		return label
	}
	return `"` + strings.ReplaceAll(label, `"`, `""`) + `"`
}

// missingLabel reports whether gh's stderr says label does not exist, in
// either of the forms gh prints.
func missingLabel(stderr, label string) bool {
	stderr = strings.ToLower(stderr)
	return strings.Contains(stderr, "'"+strings.ToLower(label)+"' not found") ||
		strings.Contains(stderr, "labels not found")
}
