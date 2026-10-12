package github

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The fields of the stack reading's three rounds, each read through
// issue(number:) or repository.pullRequests, never through a timeline
// nested in repository.issues, which GitHub has served hours stale. Each
// is aliased and takes its variable under the same name.
//
// The first reads a candidate's blockers, counting closed ones in
// totalBlockedBy, and its parent's sub-issues. The second reads the pull
// requests a blocker's timeline links to it: cross-referenced, connected in
// its Development sidebar or disconnected from it; a connect or disconnect
// event's pull request is its source or its subject, whichever is one. The
// third reads the open pull requests based on a branch.
const (
	candidateField = `
    %[1]s: issue(number: $%[1]s) {
      issueDependenciesSummary { blockedBy totalBlockedBy }
      blockedBy(first: 1) {
        nodes { number url state repository { id } labels(first: 100) { nodes { name } } }
      }
      parent { subIssues(first: 100) { nodes { number repository { id } } } }
    }`
	blockerField = `
    %[1]s: issue(number: $%[1]s) {
      timelineItems(last: 100, itemTypes: [CROSS_REFERENCED_EVENT, CONNECTED_EVENT, DISCONNECTED_EVENT]) {
        nodes {
          __typename
          ... on CrossReferencedEvent { source { ...linkedPullRequest } }
          ... on ConnectedEvent { source { ...linkedPullRequest } subject { ...linkedPullRequest } }
          ... on DisconnectedEvent { source { ...linkedPullRequest } subject { ...linkedPullRequest } }
        }
      }
    }`
	linkedFragment = `fragment linkedPullRequest on PullRequest {
  number url state isDraft headRefName baseRefName isCrossRepository repository { id }
}`
	basedField = `
    %[1]s: pullRequests(first: 20, baseRefName: $%[1]s, states: OPEN) {
      nodes { headRefName baseRefName state isCrossRepository }
    }`
)

// The GraphQL types of the rounds' variables.
const (
	intVar    = "Int!"
	stringVar = "String!"
)

// The timeline events that link a pull request to a blocker by hand.
const (
	connectedEvent    = "ConnectedEvent"
	disconnectedEvent = "DisconnectedEvent"
)

// stackCandidate is a candidate as the first round reads it.
type stackCandidate struct {
	Dependencies struct {
		BlockedBy      int `json:"blockedBy"`
		TotalBlockedBy int `json:"totalBlockedBy"`
	} `json:"issueDependenciesSummary"`
	BlockedBy struct {
		Nodes []stackIssue `json:"nodes"`
	} `json:"blockedBy"`
	Parent *struct {
		SubIssues struct {
			Nodes []stackIssue `json:"nodes"`
		} `json:"subIssues"`
	} `json:"parent"`
}

// stackIssue is a blocker or a sub-issue as the first round reads it; a
// sub-issue has only its number and repository.
type stackIssue struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Repository struct {
		ID string `json:"id"`
	} `json:"repository"`
	Labels struct {
		Nodes []ghLabel `json:"nodes"`
	} `json:"labels"`
}

// id returns the issue's crew.IssueID.
func (i stackIssue) id() crew.IssueID {
	return crew.IssueID{Key: strconv.Itoa(i.Number), Repository: crew.RepositoryID(i.Repository.ID)}
}

// blocker returns the candidate's only blocker, when exactly one issue ever
// blocked it, still open and in repository repo.
func (c stackCandidate) blocker(repo string) (stackIssue, bool) {
	if c.Dependencies.TotalBlockedBy != 1 || c.Dependencies.BlockedBy != 1 || len(c.BlockedBy.Nodes) != 1 {
		return stackIssue{}, false
	}
	b := c.BlockedBy.Nodes[0]
	return b, b.State == stateOpen && b.Repository.ID == repo
}

// siblings returns the sub-issues of the candidate's parent, none without
// one.
func (c stackCandidate) siblings() []crew.IssueID {
	if c.Parent == nil {
		return nil
	}
	ids := make([]crew.IssueID, 0, len(c.Parent.SubIssues.Nodes))
	for _, n := range c.Parent.SubIssues.Nodes {
		ids = append(ids, n.id())
	}
	return ids
}

// stackTimeline is a blocker's timeline as the second round reads it.
type stackTimeline struct {
	Items struct {
		Nodes []struct {
			Typename string            `json:"__typename"`
			Source   linkedPullRequest `json:"source"`
			Subject  linkedPullRequest `json:"subject"`
		} `json:"nodes"`
	} `json:"timelineItems"`
}

// linkedPullRequest is a pull request a blocker's timeline links to it.
// An issue that mentions the blocker reads as one with number 0.
type linkedPullRequest struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	State      string `json:"state"`
	Draft      bool   `json:"isDraft"`
	Head       string `json:"headRefName"`
	Base       string `json:"baseRefName"`
	Fork       bool   `json:"isCrossRepository"`
	Repository struct {
		ID string `json:"id"`
	} `json:"repository"`
}

// pullRequests returns the open pull requests the timeline links to the
// blocker, from a branch of repository repo into it, in number order, each
// once and connected when its latest connect or disconnect event is a
// connect. Connection arguments are not filters, so each node's state and
// repository are checked.
func (tl stackTimeline) pullRequests(repo string) []crew.StackPullRequest {
	var linked []linkedPullRequest
	connected := map[int]bool{}
	for _, e := range tl.Items.Nodes {
		p := e.Source
		if p.Number == 0 {
			p = e.Subject
		}
		if p.Number == 0 || p.State != stateOpen || p.Fork || p.Repository.ID != repo {
			continue
		}
		if !slices.ContainsFunc(linked, func(l linkedPullRequest) bool { return l.Number == p.Number }) {
			linked = append(linked, p)
		}
		switch e.Typename {
		case connectedEvent:
			connected[p.Number] = true
		case disconnectedEvent:
			connected[p.Number] = false
		}
	}
	slices.SortFunc(linked, func(a, b linkedPullRequest) int { return cmp.Compare(a.Number, b.Number) })
	if len(linked) == 0 {
		return nil
	}
	prs := make([]crew.StackPullRequest, 0, len(linked))
	for _, p := range linked {
		prs = append(prs, crew.StackPullRequest{Ref: "#" + strconv.Itoa(p.Number), URL: p.URL,
			Head: p.Head, Base: p.Base, Draft: p.Draft, Connected: connected[p.Number]})
	}
	return prs
}

// stackBased is the third round's pull requests based on a branch.
type stackBased struct {
	Nodes []struct {
		Head  string `json:"headRefName"`
		Base  string `json:"baseRefName"`
		State string `json:"state"`
		Fork  bool   `json:"isCrossRepository"`
	} `json:"nodes"`
}

// heads returns the head branches of the open pull requests from the
// repository's own branches based on branch.
func (b stackBased) heads(branch string) []string {
	var heads []string
	for _, n := range b.Nodes {
		if n.State == stateOpen && !n.Fork && n.Base == branch {
			heads = append(heads, n.Head)
		}
	}
	return heads
}

// FindStacks implements port.StackFinder with KTD2's three aliased GraphQL
// queries, each skipped when it has nothing to read: the candidates'
// blockers and parents' sub-issues, then each distinct blocker's timeline,
// then the pull requests based on each distinct head branch kept. An issue
// stacks only when exactly one issue ever blocked it, still open and in
// the repository; any other gets the zero StackReading. A blocker's labels
// are its crew states, as List reads them. An issue GitHub cannot resolve
// is left out; any other error fails the read with gh's message.
func (t *Tracker) FindStacks(ctx context.Context, issues []crew.Issue) ([]crew.Issue, error) {
	if len(issues) == 0 {
		return nil, nil
	}
	keys := make([]string, len(issues))
	for i, issue := range issues {
		keys[i] = issue.ID().Key
	}
	repo, found, err := readAliases[stackCandidate](ctx, t.gh, intVar, candidateField, "", keys)
	if err != nil {
		return nil, fmt.Errorf("read the stacks: find the blockers: %w", err)
	}
	blockers := make([]stackIssue, len(issues))
	var blockerKeys []string
	for i, c := range found {
		if c == nil {
			continue
		}
		if b, ok := c.blocker(repo); ok {
			blockers[i] = b
			blockerKeys = appendOnce(blockerKeys, strconv.Itoa(b.Number))
		}
	}
	prs, err := t.linkedPullRequests(ctx, repo, blockerKeys)
	if err != nil {
		return nil, fmt.Errorf("read the stacks: %w", err)
	}
	var out []crew.Issue
	for i, issue := range issues {
		if found[i] == nil {
			continue
		}
		d := issue.Data()
		d.Stacking = crew.StackReading{}
		if pulls, ok := prs[strconv.Itoa(blockers[i].Number)]; ok {
			d.Stacking = crew.StackReading{
				Blocker:  crew.Some(t.stackBlocker(blockers[i], pulls)),
				Siblings: found[i].siblings(),
			}
		}
		out = append(out, crew.NewIssue(d))
	}
	return out, nil
}

// linkedPullRequests returns, by blocker key, the pull requests linked to
// each of the blockers keys names that GitHub resolved, each with the head
// branches of the pull requests based on it: the second and third rounds.
func (t *Tracker) linkedPullRequests(
	ctx context.Context, repo string, keys []string,
) (map[string][]crew.StackPullRequest, error) {
	timelines, err := readRound[stackTimeline](ctx, t.gh, intVar, blockerField, linkedFragment, keys)
	if err != nil {
		return nil, fmt.Errorf("find the blockers' pull requests: %w", err)
	}
	prs := map[string][]crew.StackPullRequest{}
	var heads []string
	for _, key := range keys {
		if tl, ok := timelines[key]; ok {
			prs[key] = tl.pullRequests(repo)
			for _, p := range prs[key] {
				heads = appendOnce(heads, p.Head)
			}
		}
	}
	stacked, err := readRound[stackBased](ctx, t.gh, stringVar, basedField, "", heads)
	if err != nil {
		return nil, fmt.Errorf("find the pull requests based on them: %w", err)
	}
	for _, pulls := range prs {
		for i, p := range pulls {
			if based, ok := stacked[p.Head]; ok {
				pulls[i].Stacked = based.heads(p.Head)
			}
		}
	}
	return prs, nil
}

// stackBlocker returns the blocker b, linked to pulls, in the crew states
// its labels name, each once, in label order.
func (t *Tracker) stackBlocker(b stackIssue, pulls []crew.StackPullRequest) crew.StackBlocker {
	blocker := crew.StackBlocker{ID: b.id(), Ref: "#" + strconv.Itoa(b.Number), URL: b.URL, PullRequests: pulls}
	_, blocker.States = t.crewLabels(b.Labels.Nodes)
	return blocker
}

// readRound runs one of the later rounds over values and returns each
// value's reply that GitHub resolved, by value; it runs nothing without a
// value.
func readRound[T any](
	ctx context.Context, g *gh, typ, field, fragment string, values []string,
) (map[string]*T, error) {
	byValue := map[string]*T{}
	if len(values) == 0 {
		return byValue, nil
	}
	_, replies, err := readAliases[T](ctx, g, typ, field, fragment, values)
	if err != nil {
		return nil, err
	}
	for i, r := range replies {
		if r != nil {
			byValue[values[i]] = r
		}
	}
	return byValue, nil
}

// readAliases runs aliasedArgs's query and returns the repository's id and
// each value's reply, nil for one GitHub could not resolve: a reply whose
// only errors are NOT_FOUND is read, and any other error fails with gh's
// message.
func readAliases[T any](
	ctx context.Context, g *gh, typ, field, fragment string, values []string,
) (string, []*T, error) {
	out, callErr := g.call(ctx, aliasedArgs(typ, field, fragment, values)...)
	var reply struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(out.Stdout, &reply); err != nil {
		if callErr != nil {
			return "", nil, callErr
		}
		return "", nil, fmt.Errorf("gh api graphql: unreadable output: %w", err)
	}
	if callErr != nil && (len(reply.Errors) == 0 || slices.ContainsFunc(reply.Errors, graphQLError.fatal)) {
		return "", nil, callErr
	}
	var id string
	if raw, ok := reply.Data.Repository["id"]; !ok || json.Unmarshal(raw, &id) != nil || id == "" {
		return "", nil, errors.Join(errors.New("gh api graphql printed no repository"), callErr)
	}
	replies := make([]*T, len(values))
	for i := range values {
		raw := reply.Data.Repository["a"+strconv.Itoa(i)]
		if len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var r T
		if err := json.Unmarshal(raw, &r); err != nil {
			return "", nil, fmt.Errorf("gh api graphql: unreadable output: %w", err)
		}
		replies[i] = &r
	}
	return id, replies, nil
}

// aliasedArgs returns the gh arguments of one query on the repository: its
// id, and field once per value, aliased a0, a1… and with the variable of the
// same name, of type typ, followed by fragment.
func aliasedArgs(typ, field, fragment string, values []string) []string {
	var vars, fields strings.Builder
	args := make([]string, 0, fieldArgs*len(values))
	flag := "-F" // an Int, which gh sends as one only through -F
	if typ != intVar {
		flag = "-f" // a String, raw
	}
	for i, v := range values {
		a := "a" + strconv.Itoa(i)
		fmt.Fprintf(&vars, ", $%s: %s", a, typ)
		fmt.Fprintf(&fields, field, a)
		args = append(args, flag, a+"="+v)
	}
	query := `query($owner: String!, $name: String!` + vars.String() + `) {
  repository(owner: $owner, name: $name) {
    id` + fields.String() + `
  }
}
` + fragment
	return slices.Concat([]string{"api", "graphql", "-f", "query=" + query,
		// gh fills {owner} and {repo} from the repository, through -F only.
		"-F", "owner={owner}", "-F", "name={repo}"}, args)
}

// graphQLError is an error of a GraphQL reply.
type graphQLError struct {
	Type string `json:"type"`
}

// fatal reports whether the error is anything but GitHub not resolving an
// alias's number.
func (e graphQLError) fatal() bool { return e.Type != "NOT_FOUND" }

// appendOnce appends v to list unless it holds it.
func appendOnce(list []string, v string) []string {
	if slices.Contains(list, v) {
		return list
	}
	return append(list, v)
}
