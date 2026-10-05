// Package github is the tracker adapter for GitHub issues and pull requests,
// through the gh CLI. A rule state is the label of the same name, compared
// ignoring case as GitHub does, and crew's labels are the rules' states; it
// never touches another label. It lists the open issues
// and pull requests the code owners or one of crew's bots opened, as items
// alike, and lists the board's issues the same authors opened, whatever their
// labels. It moves items by swapping crew's labels, reports failures as
// Markdown comments and keeps a status comment on each item, with one entry
// per rule run. It puts the open pull requests that close an issue in the
// issue's crew label, and comments on them when a rule ends that nobody
// watches them any more. It finds the pull request an action opened from its
// branch. It works on the repository gh resolves from crew's working
// directory, and runs every gh call through the shared process helper.
//
// The code owners are every user the catch-all rule of the repository's
// CODEOWNERS names, or gh's login without one. The tracker reads as gh's login
// and writes as the bot the engine hands it, if any, falling back to gh's
// login when that bot cannot write.
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
// port.StatusReporter, a port.PullRequestReporter, a port.PullRequestFinder,
// a port.Acting, a port.CodeOwnerFinder, a port.LoginFinder, a
// port.WriterReporter and a port.BoardLister.
var (
	_ port.Tracker             = (*Tracker)(nil)
	_ port.Preparer            = (*Tracker)(nil)
	_ port.StatusReporter      = (*Tracker)(nil)
	_ port.PullRequestReporter = (*Tracker)(nil)
	_ port.PullRequestFinder   = (*Tracker)(nil)
	_ port.Acting              = (*Tracker)(nil)
	_ port.CodeOwnerFinder     = (*Tracker)(nil)
	_ port.LoginFinder         = (*Tracker)(nil)
	_ port.WriterReporter      = (*Tracker)(nil)
	_ port.BoardLister         = (*Tracker)(nil)
)

// issueFields are what issuesQuery reads of an issue. A dependency
// summary's blockedBy counts only the open issues blocking it. An issue
// holds at most one value per issue field, and an organization has at most
// 25 fields. A single select value carries its option's id and its field,
// with the field's options in order. The query must not ask for the values'
// totalCount: on a repository a user owns, that fails the query.
const issueFields = `fragment issueFields on Issue {
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
}`

// pullRequestsAlias is the field of issuesQuery's reply that lists the pull
// requests; issuesAlias followed by an author's index lists that author's
// issues.
const (
	pullRequestsAlias = "pullRequests"
	issuesAlias       = "issues"
)

// issuesQuery returns the query listing the open issues each of authors
// opened carrying any of the labels, one aliased issues field per author,
// and, when pullRequests is set, the open pull requests carrying any of
// them, each list oldest first. GitHub's labels filter matches an item with
// any of them, ignoring case, and matches nothing for a label the
// repository lacks. Pull requests cannot be filtered by author, so each
// carries its author.
func issuesQuery(authors int, pullRequests bool) string {
	var vars, fields strings.Builder
	for i := range authors {
		fmt.Fprintf(&vars, ", $author%d: String!", i)
		fmt.Fprintf(&fields, `
    %s%d: issues(first: 100, states: OPEN, filterBy: {createdBy: $author%d, labels: $labels},
           orderBy: {field: CREATED_AT, direction: ASC}) { nodes { ...issueFields } }`, issuesAlias, i, i)
	}
	if pullRequests {
		fields.WriteString(`
    ` + pullRequestsAlias + `: pullRequests(first: 100, states: OPEN, labels: $labels,
                 orderBy: {field: CREATED_AT, direction: ASC}) {
      nodes {
        number
        title
        url
        createdAt
        author { __typename login }
        labels(first: 100) { nodes { name } }
      }
    }`)
	}
	return `query($owner: String!, $name: String!, $labels: [String!]` + vars.String() + `) {
  repository(owner: $owner, name: $name) {` + fields.String() + `
  }
}
` + issueFields
}

// issuesReply is issuesQuery's reply: its repository's fields by alias.
type issuesReply struct {
	Data struct {
		Repository map[string]struct {
			Nodes []listNode `json:"nodes"`
		} `json:"repository"`
	} `json:"data"`
}

// issues returns the issue nodes of r, an issuesQuery reply for authors
// authors, author by author, each issue once.
func (r issuesReply) issues(authors int) []listNode {
	var nodes []listNode
	seen := map[int]bool{}
	for i := range authors {
		for _, n := range r.Data.Repository[issuesAlias+strconv.Itoa(i)].Nodes {
			if !seen[n.Number] {
				seen[n.Number] = true
				nodes = append(nodes, n)
			}
		}
	}
	return nodes
}

// listNode is an issue or a pull request as issuesQuery reads it. An
// issue's has no author, and a pull request's no dependencies and no field
// values.
type listNode struct {
	itemNode

	Dependencies struct {
		BlockedBy int `json:"blockedBy"`
	} `json:"issueDependenciesSummary"`
	FieldValues struct {
		Nodes []fieldValue `json:"nodes"`
	} `json:"issueFieldValues"`
	Author *struct {
		Typename string `json:"__typename"`
		Login    string `json:"login"`
	} `json:"author"`
}

// authorLogin returns the login of n's author as GitHub's REST API and
// issue filters write it: a bot's with [bot] after it, so the user crew-ops
// and the bot crew-ops[bot] differ. A deleted account has none.
func (n listNode) authorLogin() string {
	switch {
	case n.Author == nil:
		return ""
	case n.Author.Typename == "Bot":
		return n.Author.Login + "[bot]"
	default:
		return n.Author.Login
	}
}

// itemNode holds what issuesQuery reads of an issue and of a pull request
// alike.
type itemNode struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	Labels    struct {
		Nodes []ghLabel `json:"nodes"`
	} `json:"labels"`
}

// ghLabel is a label as gh prints it in JSON.
type ghLabel struct {
	Name string `json:"name"`
}

// Tracker is the GitHub tracker. It is safe for concurrent use.
type Tracker struct {
	gh     *gh
	labels labels

	mu         sync.Mutex
	comments   map[string]cachedStatus // status comments by issue key, as last written or read
	stopped    map[string][]int        // pull requests given a report's stop comment, by report ID
	codeOwners []string                // the code owners' logins, once Prepare found them
	bots       []string                // the logins of the bots the config names
}

// Factory returns the github tracker's factory, which runs gh through group.
// The tracker section has no key, so the factory refuses any. The tracker
// knows the rules' states as its labels. It runs no gh
// call; Prepare does.
func Factory(group *proc.Group) port.TrackerFactory {
	return factory(group.Run)
}

func factory(run proc.Runner) port.TrackerFactory {
	return func(decode port.Decode, states []crew.State) (port.Tracker, error) {
		if err := decode(&settings{}); err != nil {
			return nil, err
		}
		return &Tracker{gh: &gh{run: run}, labels: newLabels(states),
			comments: map[string]cachedStatus{}, stopped: map[string][]int{}}, nil
	}
}

// List implements port.Tracker with one GraphQL query: the open issues that
// carry any of the states' labels and that the code owners or one of the bots
// opened, at most 100 per author, and the open pull requests the code owners
// or one of the bots opened that carry any of them, among the 100 oldest pull
// requests carrying any of them, all oldest first. Before Prepare found the
// code owners, the code owner is gh's login. An issue two authors' lists hold
// counts once. Each item's key is its number, its reference #<number>, its
// kind issue or pull request, and its states every rule state its labels name,
// in the rules' spelling. Its other labels are no states and are ignored. An
// issue is blocked while an open issue blocks it, as GitHub's issue
// dependencies record. Its priority is the position of its value of the issue
// field Priority among that field's options, the first being 1; an issue
// without one has priority 0. A pull request has priority 0 and is never
// blocked.
func (t *Tracker) List(ctx context.Context, states []crew.State) ([]crew.Issue, error) {
	authors, err := t.authors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	labels := make([]string, len(states))
	for i, s := range states {
		labels[i] = string(s)
	}
	var reply issuesReply
	if err := t.gh.decode(ctx, &reply, issuesArgs(authors, labels, true)...); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	var items []crew.Issue
	for _, n := range reply.issues(len(authors)) {
		items = append(items, t.issue(n))
	}
	for _, n := range reply.Data.Repository[pullRequestsAlias].Nodes {
		if login := n.authorLogin(); login != "" && containsFold(authors, login) {
			pr := t.item(n.itemNode)
			pr.Kind = crew.KindPullRequest
			items = append(items, pr)
		}
	}
	slices.SortStableFunc(items, func(a, b crew.Issue) int { return a.Created.Compare(b.Created) })
	return items, nil
}

// ListBoard implements port.BoardLister with List's query without its pull
// requests: the open issues the code owners or one of the bots opened that
// carry any of labels, at most 100 per author, oldest first, each as List
// returns it. Each carries the labels of labels its own labels match ignoring
// case, as GitHub compares them, in labels' spelling and order. An issue none
// of whose labels matches, which GitHub's filter should not return, is left
// out.
func (t *Tracker) ListBoard(ctx context.Context, labels []string) ([]crew.BoardIssue, error) {
	authors, err := t.authors(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the board's issues: %w", err)
	}
	var reply issuesReply
	if err := t.gh.decode(ctx, &reply, issuesArgs(authors, labels, false)...); err != nil {
		return nil, fmt.Errorf("list the board's issues: %w", err)
	}
	var board []crew.BoardIssue
	for _, n := range reply.issues(len(authors)) {
		var carried []string
		for _, l := range labels {
			if slices.ContainsFunc(n.Labels.Nodes, func(g ghLabel) bool { return strings.EqualFold(g.Name, l) }) {
				carried = append(carried, l)
			}
		}
		if len(carried) > 0 {
			board = append(board, crew.BoardIssue{Issue: t.issue(n), Labels: carried})
		}
	}
	slices.SortStableFunc(board, func(a, b crew.BoardIssue) int { return a.Issue.Created.Compare(b.Issue.Created) })
	return board, nil
}

// ActAs implements port.Acting: the tracker's writes go as writer, you
// when it is the zero Identity, and List also takes the items the logins in
// bots opened.
func (t *Tracker) ActAs(writer port.Identity, bots []string) {
	t.gh.actAs(writer)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bots = slices.Clone(bots)
}

// CodeOwners implements port.CodeOwnerFinder: the code owners' logins as
// Prepare found them, none before.
func (t *Tracker) CodeOwners() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.codeOwners)
}

// Login implements port.LoginFinder: gh's own login, which the tracker reads
// as and writes as when no bot does, as Prepare found it, "" before.
func (t *Tracker) Login() string {
	return t.gh.known()
}

// WriterLost implements port.WriterReporter: the warning crew wrote when the
// writes went back to you for the rest of the run, "" while they go as
// the bot, or no bot writes.
func (t *Tracker) WriterLost() string {
	return t.gh.writerLost()
}

// fieldArgs is how many arguments one gh api field takes: the flag and
// key=value.
const fieldArgs = 2

// stateOpen is the state GitHub gives an open issue or pull request.
const stateOpen = "OPEN"

// issuesArgs returns the gh arguments of issuesQuery, for the issues of
// authors carrying any of labels and, when pullRequests is set, the pull
// requests carrying any of them.
func issuesArgs(authors, labels []string, pullRequests bool) []string {
	vars := make([]string, 0, fieldArgs*(len(labels)+len(authors)))
	for _, l := range labels {
		vars = append(vars, "-f", "labels[]="+l)
	}
	for i, a := range authors {
		vars = append(vars, "-f", "author"+strconv.Itoa(i)+"="+a)
	}
	return slices.Concat([]string{"api", "graphql",
		"-f", "query=" + issuesQuery(len(authors), pullRequests),
		// gh fills {owner} and {repo} from the repository, through -F only.
		"-F", "owner={owner}", "-F", "name={repo}"}, vars)
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

// Move implements port.Tracker. It moves a pull request as it moves an issue,
// since gh issue view and gh issue edit accept a pull request's number. It
// reads the issue's state and labels; a closed issue, or a closed or merged
// pull request, moved meanwhile. An open issue whose only state label is
// to's, whatever other labels it carries, is already moved, as when an earlier
// attempt landed although gh reported an error, so Move returns nil without
// an edit and a retry is safe (KTD8). Any other issue without from's label
// moved meanwhile. Otherwise one gh issue edit removes every other crew label
// the issue carries and adds to's, leaving the labels that are not crew's,
// those no rule names, alone. gh saying a label does not exist is a refusal: the
// label must be created, which retrying cannot do.
func (t *Tracker) Move(ctx context.Context, issueKey string, from, to crew.State) error {
	var issue struct {
		State  string    `json:"state"`
		Labels []ghLabel `json:"labels"`
	}
	move := fmt.Sprintf("move issue #%s from %s to %s", issueKey, from, to)
	if err := t.gh.decode(ctx, &issue, "issue", "view", issueKey, "--json", "state,labels"); err != nil {
		return fmt.Errorf("%s: %w", move, err)
	}
	if issue.State != stateOpen {
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

// ReportFailure implements port.Tracker: one Markdown comment, posted as the
// writer, naming each failed action and its log, without its reason. The
// issue gone (HTTP 404 or 410) is port.ErrMovedMeanwhile, a refusal (HTTP
// 403, such as a locked issue, but not a rate limit) is port.ErrRefused, and
// any other error is transient.
func (t *Tracker) ReportFailure(ctx context.Context, report crew.FailureReport) error {
	if _, _, err := t.postComment(ctx, report.IssueKey, renderReport(report)); err != nil {
		return fmt.Errorf("report failure on issue #%s: %w", report.IssueKey, err)
	}
	return nil
}

// Prepare implements port.Preparer. It checks that gh is installed and logged
// in, then finds the code owners in CODEOWNERS, then creates the labels of
// states the repository lacks, comparing names
// case-insensitively, and no other label. It reads as you and creates the
// labels as the writer. It reports each step on ctx as it starts, one per
// label it creates.
func (t *Tracker) Prepare(ctx context.Context, states []crew.State) error {
	port.Step(ctx, "checking the gh login")
	if _, err := t.gh.call(ctx, "auth", "status"); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return fmt.Errorf("tracker github needs the gh CLI, which is not on PATH: %w", err)
		}
		return fmt.Errorf("tracker github: gh is not logged in to GitHub; run `gh auth login`: %w", err)
	}
	port.Step(ctx, "finding the code owners")
	codeOwners, err := t.findCodeOwners(ctx)
	if err != nil {
		return fmt.Errorf("tracker github: find the code owners: %w", err)
	}
	t.mu.Lock()
	t.codeOwners = codeOwners
	t.mu.Unlock()
	port.Step(ctx, "reading the repository's labels")
	var present []ghLabel
	if err := t.gh.decode(ctx, &present, "label", "list", "--limit", "1000", "--json", "name"); err != nil {
		return fmt.Errorf("tracker github: read the repository's labels: %w", err)
	}
	have := make(map[string]bool, len(present))
	for _, l := range present {
		have[strings.ToLower(l.Name)] = true
	}
	for _, s := range states {
		name := string(s)
		if have[strings.ToLower(name)] {
			continue
		}
		port.Step(ctx, fmt.Sprintf("creating the label %q", name))
		if _, _, err := t.gh.write(ctx, "label", "create", name); err != nil {
			return fmt.Errorf("tracker github: create the label %q: %w", name, err)
		}
		have[strings.ToLower(name)] = true
	}
	return nil
}

// authors returns the logins whose items List takes: the code owners', gh's
// login until Prepare found them, then the bots', each once, ignoring case.
func (t *Tracker) authors(ctx context.Context) ([]string, error) {
	t.mu.Lock()
	codeOwners, bots := slices.Clone(t.codeOwners), slices.Clone(t.bots)
	t.mu.Unlock()
	if len(codeOwners) == 0 {
		login, err := t.gh.viewer(ctx)
		if err != nil {
			return nil, err
		}
		codeOwners = []string{login}
	}
	return appendFold(codeOwners, bots...), nil
}

// item returns the issue or pull request n as a crew.Issue in the states its
// labels name, each once, in label order.
func (t *Tracker) item(n itemNode) crew.Issue {
	key := strconv.Itoa(n.Number)
	issue := crew.Issue{Key: key, Ref: "#" + key, Title: n.Title, URL: n.URL, Created: n.CreatedAt}
	for _, l := range n.Labels.Nodes {
		if s, ok := t.labels.stateOf(l.Name); ok && !slices.Contains(issue.States, s) {
			issue.States = append(issue.States, s)
		}
	}
	return issue
}

// issue returns the issue n as a crew.Issue, as item returns it, blocked
// while an open issue blocks it and ranked by its Priority value.
func (t *Tracker) issue(n listNode) crew.Issue {
	issue := t.item(n.itemNode)
	issue.Blocked = n.Dependencies.BlockedBy > 0
	issue.Priority = priority(n.FieldValues.Nodes)
	return issue
}

// editLabels runs one gh <kind> edit of number, an issue's or a pull
// request's, that removes the labels remove names, as swap returns them, and
// adds to's. gh saying to's label does not exist is a refusal: the label must
// be created, which retrying cannot do.
func (t *Tracker) editLabels(ctx context.Context, kind, number string, remove []string, to crew.State) error {
	target := string(to)
	args := slices.Concat([]string{kind, "edit", number}, remove, []string{"--add-label=" + labelArg(target)})
	if out, _, err := t.gh.write(ctx, args...); err != nil {
		if missingLabel(string(out.Stderr), target) {
			return fmt.Errorf("%w: %w", port.ErrRefused, err)
		}
		return err
	}
	return nil
}

// swap returns the --remove-label arguments that take every crew label but
// to's off a labelable carrying labels, leaving the labels that are not
// crew's, and the states labels name, each once, in label order.
// Move and the pull request mirror both swap labels through it.
func (t *Tracker) swap(labels []ghLabel, to crew.State) ([]string, []crew.State) {
	var remove []string
	var states []crew.State
	for _, l := range labels {
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
