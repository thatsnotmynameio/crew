package github

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The repositories of the stack tests: crew's, and another one.
const (
	stackRepo  = "R_repo"
	otherRepo  = "R_other"
	blockerURL = "https://github.com/o/r/issues/10"
)

// stackRound is the reply to one of FindStacks's queries, answering once.
func stackRound(aliases ...string) reply {
	return reply{prefix: prQuery, once: true, stdout: stackReply(aliases...)}
}

// stackReply is a query's reply on crew's repository with the given
// aliased fields, each written `"a0":{...}`.
func stackReply(aliases ...string) string {
	fields := append([]string{`"id":"` + stackRepo + `"`}, aliases...)
	return `{"data":{"repository":{` + strings.Join(fields, ",") + `}}}`
}

// alias is the field of a query's reply for its index'th value.
func alias(index int, value string) string { return fmt.Sprintf(`"a%d":%s`, index, value) }

// candidate is a candidate as the first round reads it: its open and total
// blockers, the first blocker's node or "" for none, and its parent or
// "null".
func candidate(open, total int, blocker, parent string) string {
	nodes := ""
	if blocker != "" {
		nodes = blocker
	}
	return fmt.Sprintf(`{"issueDependenciesSummary":{"blockedBy":%d,"totalBlockedBy":%d},`+
		`"blockedBy":{"nodes":[%s]},"parent":%s}`, open, total, nodes, parent)
}

// blockerNode is #10 blocking a candidate, in repository repo.
func blockerNode(state, repo string, labels ...crew.State) string {
	names := make([]string, len(labels))
	for i, l := range labels {
		names[i] = fmt.Sprintf(`{"name":%q}`, l)
	}
	return fmt.Sprintf(`{"number":10,"url":%q,"state":%q,"repository":{"id":%q},"labels":{"nodes":[%s]}}`,
		blockerURL, state, repo, strings.Join(names, ","))
}

// parent is a candidate's parent with its sub-issues, each "number@repo".
func parent(subs ...string) string {
	nodes := make([]string, len(subs))
	for i, s := range subs {
		number, repo, _ := strings.Cut(s, "@")
		nodes[i] = fmt.Sprintf(`{"number":%s,"repository":{"id":%q}}`, number, repo)
	}
	return `{"subIssues":{"nodes":[` + strings.Join(nodes, ",") + `]}}`
}

// timeline is a blocker's timeline with the given events.
func timeline(events ...string) string {
	return `{"timelineItems":{"nodes":[` + strings.Join(events, ",") + `]}}`
}

// The timeline's events, each linking pr to the blocker.
func crossReferenced(pr string) string {
	return `{"__typename":"CrossReferencedEvent","source":` + pr + `}`
}

func connected(pr string) string {
	return `{"__typename":"ConnectedEvent","source":{},"subject":` + pr + `}`
}

func disconnected(pr string) string {
	return `{"__typename":"DisconnectedEvent","source":{},"subject":` + pr + `}`
}

// mentioned is a cross-reference from an issue, which reads as an empty
// pull request.
const mentioned = `{"__typename":"CrossReferencedEvent","source":{}}`

// pr is a pull request linked to a blocker.
type pr struct {
	number      int
	state       string
	head, base  string
	draft, fork bool
	repo        string
}

func (p pr) json() string {
	return fmt.Sprintf(`{"number":%d,"url":"https://github.com/o/r/pull/%d","state":%q,"isDraft":%t,`+
		`"headRefName":%q,"baseRefName":%q,"isCrossRepository":%t,"repository":{"id":%q}}`,
		p.number, p.number, p.state, p.draft, p.head, p.base, p.fork, p.repo)
}

// openPR is an open pull request of crew's repository from head into main.
func openPR(number int, head string) pr {
	return pr{number: number, state: stateOpen, head: head, base: "main", repo: stackRepo}
}

// based lists the pull requests based on a branch, each with its head,
// base, state and whether it comes from a fork.
func based(prs ...pr) string {
	nodes := make([]string, len(prs))
	for i, p := range prs {
		nodes[i] = fmt.Sprintf(`{"headRefName":%q,"baseRefName":%q,"state":%q,"isCrossRepository":%t}`,
			p.head, p.base, p.state, p.fork)
	}
	return `{"nodes":[` + strings.Join(nodes, ",") + `]}`
}

// candidates are blocked issues of crew's repository with the given
// numbers.
func candidates(keys ...string) []crew.Issue {
	issues := make([]crew.Issue, len(keys))
	for i, k := range keys {
		issues[i] = crew.NewIssue(crew.IssueData{
			ID: crew.IssueID{Key: k, Repository: stackRepo}, Ref: "#" + k, Blocked: true,
			States: []crew.State{"crew:ready for development"},
		})
	}
	return issues
}

// inRepo is the ID of issue key in repository repo.
func inRepo(key string, repo crew.RepositoryID) crew.IssueID {
	return crew.IssueID{Key: key, Repository: repo}
}

// readings returns each issue's key and stack reading.
func readings(issues []crew.Issue) map[string]crew.StackReading {
	out := map[string]crew.StackReading{}
	for _, i := range issues {
		out[i.ID().Key] = i.Data().Stacking
	}
	return out
}

// mixedTimeline is #10's timeline: #20, the open pull request from head
// into main, and #24, an open draft from person/draft into develop, among
// links that do not count: a merged pull request, a fork's, an issue's
// mention and another repository's.
func mixedTimeline(head string) string {
	merged := openPR(21, "crew/issue-10-old")
	merged.state = "MERGED"
	fork := openPR(22, "main")
	fork.fork = true
	elsewhere := openPR(23, "feature")
	elsewhere.repo = otherRepo
	draft := openPR(24, "person/draft")
	draft.draft, draft.base = true, "develop"
	return timeline(crossReferenced(openPR(20, head).json()), crossReferenced(merged.json()),
		crossReferenced(fork.json()), mentioned, crossReferenced(elsewhere.json()), crossReferenced(draft.json()))
}

// mixedBased lists the pull requests based on head: one from
// crew/issue-12-development, which counts, a fork's and a closed one.
func mixedBased(head string) string {
	return based(pr{head: "crew/issue-12-development", base: head, state: stateOpen},
		pr{head: "fork/branch", base: head, state: stateOpen, fork: true},
		pr{head: "crew/issue-14-development", base: head, state: "CLOSED"})
}

func TestFindStacksReadsTheBlockerItsPullRequestsAndTheSiblings(t *testing.T) {
	head := "crew/issue-10-development"
	blocker := blockerNode(stateOpen, stackRepo, crewWaitingReview, "bug")
	tr, gh := prTracker(t,
		stackRound(
			alias(0, candidate(1, 1, blocker, parent("11@"+stackRepo, "12@"+stackRepo, "99@"+otherRepo))),
			alias(1, candidate(1, 1, blocker, "null"))),
		stackRound(alias(0, mixedTimeline(head))),
		stackRound(alias(0, mixedBased(head)), alias(1, based())),
	)

	got, err := tr.FindStacks(context.Background(), candidates("12", "13"))
	if err != nil {
		t.Fatalf("FindStacks: %v", err)
	}
	pullRequests := []crew.StackPullRequest{
		{Ref: "#20", URL: "https://github.com/o/r/pull/20", Head: head, Base: "main",
			Stacked: []string{"crew/issue-12-development"}},
		{Ref: "#24", URL: "https://github.com/o/r/pull/24", Head: "person/draft", Base: "develop", Draft: true},
	}
	reading := func(siblings ...crew.IssueID) crew.StackReading {
		return crew.StackReading{
			Blocker: crew.Some(crew.StackBlocker{
				ID: inRepo("10", stackRepo), Ref: "#10", URL: blockerURL,
				States: []crew.State{crewWaitingReview}, PullRequests: pullRequests,
			}),
			Siblings: siblings,
		}
	}
	want := map[string]crew.StackReading{
		"12": reading(inRepo("11", stackRepo), inRepo("12", stackRepo), inRepo("99", otherRepo)),
		"13": reading(),
	}
	if g := readings(got); !reflect.DeepEqual(g, want) {
		t.Errorf("readings =\n%+v\nwant\n%+v", g, want)
	}
	if got[0].ID() != inRepo("12", stackRepo) || got[1].ID() != inRepo("13", stackRepo) {
		t.Errorf("issues = %v, %v; want #12 then #13 as given", got[0].ID(), got[1].ID())
	}

	checkRounds(t, gh.callsTo(prQuery...), []map[string][]string{
		{"a0": {"12"}, "a1": {"13"}},
		{"a0": {"10"}, "a1": nil},
		{"a0": {head}, "a1": {"person/draft"}},
	})
}

// checkRounds checks that queries are one query per round, each on the
// repository gh resolves and with want's fields for its round. A number is
// an Int, which gh sends as one only through -F; a branch is a String, sent
// raw through -f.
func checkRounds(t *testing.T, queries [][]string, want []map[string][]string) {
	t.Helper()
	if len(queries) != len(want) {
		t.Fatalf("queries = %d, want one per round, %d", len(queries), len(want))
	}
	for i, fields := range want {
		fields["owner"], fields["name"] = []string{"{owner}"}, []string{"{repo}"}
		for key, values := range fields {
			if got := fieldValues(queries[i], key); !slices.Equal(got, values) {
				t.Errorf("round %d: field %s = %q, want %q", i+1, key, got, values)
			}
		}
	}
	if i := slices.Index(queries[0], "a0=12"); i < 1 || queries[0][i-1] != "-F" {
		t.Errorf("round 1 sends the number through %q, want -F", queries[0])
	}
	last := queries[len(queries)-1]
	i := slices.IndexFunc(last, func(a string) bool { return strings.HasPrefix(a, "a0=") })
	if i < 1 || last[i-1] != "-f" {
		t.Errorf("the last round sends the branch through %q, want -f", last)
	}
}

func TestFindStacksReadsNothingForAnIssueWithoutOneOpenBlockerHere(t *testing.T) {
	for name, c := range map[string]string{
		"two blockers, one of them closed": candidate(1, 2, blockerNode(stateOpen, stackRepo), "null"),
		"its only blocker closed":          candidate(0, 1, blockerNode("CLOSED", stackRepo), "null"),
		"its only blocker elsewhere":       candidate(1, 1, blockerNode(stateOpen, otherRepo), "null"),
		"no blocker":                       candidate(0, 0, "", parent("11@"+stackRepo, "12@"+stackRepo)),
	} {
		t.Run(name, func(t *testing.T) {
			tr, gh := prTracker(t, stackRound(alias(0, c)))
			got, err := tr.FindStacks(context.Background(), candidates("12"))
			if err != nil {
				t.Fatalf("FindStacks: %v", err)
			}
			if want := map[string]crew.StackReading{"12": {}}; !reflect.DeepEqual(readings(got), want) {
				t.Errorf("readings = %+v, want #12 with none", readings(got))
			}
			if n := len(gh.callsTo(prQuery...)); n != 1 {
				t.Errorf("queries = %d, want only the first round's", n)
			}
		})
	}
}

func TestFindStacksFlagsThePullRequestsAPersonConnected(t *testing.T) {
	only := openPR(30, "person/only-connected")
	undone := openPR(31, "person/connected-then-disconnected")
	referenced := openPR(32, "person/referenced")
	later := openPR(33, "person/referenced-then-connected")
	again := openPR(34, "person/disconnected-then-connected")
	tr, _ := prTracker(t,
		stackRound(alias(0, candidate(1, 1, blockerNode(stateOpen, stackRepo), "null"))),
		stackRound(alias(0, timeline(
			connected(only.json()),
			connected(undone.json()), disconnected(undone.json()),
			crossReferenced(referenced.json()),
			crossReferenced(later.json()), connected(later.json()),
			disconnected(again.json()), connected(again.json())))),
		stackRound(alias(0, based()), alias(1, based()), alias(2, based()), alias(3, based()), alias(4, based())),
	)

	got, err := tr.FindStacks(context.Background(), candidates("12"))
	if err != nil {
		t.Fatalf("FindStacks: %v", err)
	}
	blocker, ok := got[0].Data().Stacking.Blocker.Get()
	if !ok {
		t.Fatal("#12 has no blocker, want #10")
	}
	flags := map[string]bool{}
	for _, p := range blocker.PullRequests {
		flags[p.Ref] = p.Connected
	}
	want := map[string]bool{"#30": true, "#31": false, "#32": false, "#33": true, "#34": true}
	if !reflect.DeepEqual(flags, want) {
		t.Errorf("connected = %v, want %v", flags, want)
	}
}

func TestFindStacksSkipsTheLaterRoundsWithNothingToRead(t *testing.T) {
	tr, gh := prTracker(t,
		stackRound(alias(0, candidate(1, 1, blockerNode(stateOpen, stackRepo), "null"))),
		stackRound(alias(0, timeline(mentioned))),
	)
	got, err := tr.FindStacks(context.Background(), candidates("12"))
	if err != nil {
		t.Fatalf("FindStacks: %v", err)
	}
	want := crew.StackReading{Blocker: crew.Some(crew.StackBlocker{
		ID: inRepo("10", stackRepo), Ref: "#10", URL: blockerURL,
	})}
	if g := readings(got); !reflect.DeepEqual(g, map[string]crew.StackReading{"12": want}) {
		t.Errorf("readings = %+v, want #10 with no pull request and no sibling", g)
	}
	if n := len(gh.callsTo(prQuery...)); n != 2 {
		t.Errorf("queries = %d, want no third round without a pull request", n)
	}

	tr, gh = prTracker(t)
	if got, err := tr.FindStacks(context.Background(), nil); err != nil || len(got) != 0 {
		t.Errorf("FindStacks(none) = %v, %v; want none", got, err)
	}
	if calls := gh.callsTo(); len(calls) != 0 {
		t.Errorf("calls = %q, want none without a candidate", calls)
	}
}

func TestFindStacksLeavesOutAnIssueGitHubCannotResolve(t *testing.T) {
	notFound := `"errors":[{"type":"NOT_FOUND","path":["repository","a1"],` +
		`"message":"Could not resolve to an Issue with the number of 13."}]`
	reply := stackRound(alias(0, candidate(0, 0, "", "null")), alias(1, "null"))
	reply.stdout = strings.TrimSuffix(reply.stdout, "}") + "," + notFound + "}"
	reply.stderr = "gh: Could not resolve to an Issue with the number of 13."
	tr, _ := prTracker(t, reply)

	got, err := tr.FindStacks(context.Background(), candidates("12", "13"))
	if err != nil {
		t.Fatalf("FindStacks: %v", err)
	}
	if want := map[string]crew.StackReading{"12": {}}; !reflect.DeepEqual(readings(got), want) {
		t.Errorf("readings = %+v, want only #12", readings(got))
	}
}

func TestFindStacksFailsOnAGraphQLError(t *testing.T) {
	const message = "gh: Something went wrong while executing your query."
	failed := reply{prefix: prQuery, once: true,
		stdout: `{"data":null,"errors":[{"message":"Something went wrong while executing your query."}]}`,
		stderr: message}
	blocked := stackRound(alias(0, candidate(1, 1, blockerNode(stateOpen, stackRepo), "null")))
	for name, script := range map[string][]reply{
		"the first round":  {failed},
		"the second round": {blocked, failed},
		"the third round":  {blocked, stackRound(alias(0, timeline(crossReferenced(openPR(20, "b").json())))), failed},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _ := prTracker(t, script...)
			got, err := tr.FindStacks(context.Background(), candidates("12"))
			if err == nil || !strings.Contains(err.Error(), message) {
				t.Errorf("FindStacks = %v, %v; want an error with gh's message", got, err)
			}
		})
	}
}
