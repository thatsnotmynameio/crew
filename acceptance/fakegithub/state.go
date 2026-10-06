// Package fakegithub is an in-memory GitHub repository that answers the gh
// CLI's calls. A test builds a repository with New and the setup methods
// (labels, issues, pull requests, comments, files, teams), hands every gh
// invocation to Run, and reads the repository's state at the end with the
// read methods. It keeps state as GitHub does: labels, issues and pull
// requests sharing one number sequence, their comments, and the files and
// teams GitHub serves.
//
// The fake is strict. A gh call it does not know (an unknown subcommand,
// endpoint, flag, header or GraphQL field) is a violation: Run answers it
// with exit code 1, and Reply.Violation names the call. A known call on a
// missing object fails as GitHub does, with gh's error text, and is not a
// violation.
//
// Every method is safe for concurrent use, and answers are deterministic:
// comment ids count up from 1001 and every time GitHub would stamp comes
// from a fixed base time.
package fakegithub

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

// State is the state of an issue or a pull request, as GitHub's GraphQL API
// spells it.
type State string

// The states GitHub gives issues and pull requests. Merged is a pull
// request's only.
const (
	Open   State = "OPEN"
	Closed State = "CLOSED"
	Merged State = "MERGED"
)

// Issue is an issue of the repository, as a test sets it up with AddIssue
// and reads it back with Issue.
type Issue struct {
	// Number is the issue's number. AddIssue gives the next free number
	// when it is 0.
	Number int
	// Title is the issue's title.
	Title string
	// Author is the login of the account that opened the issue; AddIssue
	// uses the viewer when it is "". A GitHub App's login ends in "[bot]".
	Author string
	// Labels are the names of the issue's labels, in the order they were
	// added.
	Labels []string
	// CreatedAt is when the issue was opened. AddIssue derives a fixed time
	// from the number when it is zero.
	CreatedAt time.Time
	// State is Open or Closed; AddIssue uses Open when it is "".
	State State
	// Priority is the name of the option the issue's single-select
	// Priority field holds, "" for none. See SetPriorityOptions.
	Priority string
	// BlockedBy are the numbers of the issues that block this one, through
	// GitHub's issue dependencies. Only the open ones count as blocking.
	BlockedBy []int
}

// PullRequest is a pull request of the repository, as a test sets it up
// with AddPullRequest and reads it back with PullRequest.
type PullRequest struct {
	// Number is the pull request's number, from the sequence issues use.
	// AddPullRequest gives the next free number when it is 0.
	Number int
	// Title is the pull request's title.
	Title string
	// HeadBranch is the name of the branch the pull request merges from.
	HeadBranch string
	// State is Open, Closed or Merged; AddPullRequest uses Open when it is
	// "".
	State State
	// Author is the login of the account that opened it; AddPullRequest
	// uses the viewer when it is "".
	Author string
	// Labels are the names of its labels, in the order they were added.
	Labels []string
	// CrossRepository is whether its head branch lives in a fork.
	CrossRepository bool
	// Closes are the numbers of the issues it closes when it merges, as a
	// "Closes #N" line links them.
	Closes []int
	// CreatedAt is when it was opened. AddPullRequest derives a fixed time
	// from the number when it is zero.
	CreatedAt time.Time
}

// Comment is a comment on an issue or a pull request.
type Comment struct {
	// ID is the comment's id, as GitHub's REST API gives it.
	ID int64
	// Author is the login of the account that wrote it.
	Author string
	// Body is its Markdown text.
	Body string
}

// item is an issue or a pull request, which share GitHub's numbers.
type item struct {
	number    int
	title     string
	author    string
	labels    []string
	createdAt time.Time
	state     State
	priority  string
	blockedBy []int
	pull      *pullInfo // nil for an issue
}

// pullInfo is what a pull request holds beyond an issue.
type pullInfo struct {
	head   string
	cross  bool
	closes []int
}

// comment is a comment as the fake stores it.
type comment struct {
	id      int64
	number  int
	author  string
	body    string
	created time.Time
	updated time.Time
}

// failure is a GitHub error a test scripted with Fail.
type failure struct {
	words   []string
	status  int
	message string
	left    int
}

// GitHub is one fake GitHub repository and the account gh is logged in as.
// Build it with New.
type GitHub struct {
	mu         sync.Mutex
	owner      string
	name       string
	viewer     string
	labels     []string
	files      map[string]string
	teams      map[string][]string // members by "org/slug"
	items      map[int]*item
	comments   []*comment
	priorities []string
	failures   []*failure
	ticks      int // the minutes after baseTime of the latest write
	log        []string
	changed    chan struct{}
	commands   map[string]command
}

// New returns an empty repository owner/name: gh is logged in as boss, and
// there are no labels, files, teams, issues or pull requests. The issue
// field Priority has the options Urgent, High, Medium and Low.
func New(owner, name string) *GitHub {
	return &GitHub{
		owner:      owner,
		name:       name,
		viewer:     "boss",
		files:      map[string]string{},
		teams:      map[string][]string{},
		items:      map[int]*item{},
		priorities: []string{"Urgent", "High", "Medium", "Low"},
		changed:    make(chan struct{}),
		commands:   commands(),
	}
}

// Changed returns a channel that is closed at the next change of the
// repository's state, through Run or a setup method. Call it again after
// it fires to wait for the change after that.
func (g *GitHub) Changed() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.changed
}

// SetViewer makes login the account gh is logged in as: the one `gh api
// user` names and the author of what gh writes.
func (g *GitHub) SetViewer(login string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.viewer = login
	g.touch()
}

// AddLabel creates the labels the repository lacks among names, comparing
// names ignoring case as GitHub does.
func (g *GitHub) AddLabel(names ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ensureLabels(names)
	g.touch()
}

// SetPriorityOptions replaces the options of the single-select issue field
// Priority, in their order, most urgent first.
func (g *GitHub) SetPriorityOptions(options ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.priorities = slices.Clone(options)
	g.touch()
}

// AddIssue adds issue to the repository and returns its number. Labels the
// repository lacks are created, as GitHub's REST API does. It panics when
// the number is taken or the priority is not one of the Priority field's
// options: the test's setup is wrong.
func (g *GitHub) AddIssue(issue Issue) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if issue.Priority != "" && !slices.Contains(g.priorities, issue.Priority) {
		panic(fmt.Sprintf("fakegithub: issue priority %q is not one of %q", issue.Priority, g.priorities))
	}
	it := &item{title: issue.Title, author: issue.Author, labels: issue.Labels, createdAt: issue.CreatedAt,
		state: issue.State, priority: issue.Priority, blockedBy: slices.Clone(issue.BlockedBy)}
	return g.add(issue.Number, it)
}

// AddPullRequest adds pr to the repository and returns its number. Labels
// the repository lacks are created. It panics when the number is taken.
func (g *GitHub) AddPullRequest(pr PullRequest) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	it := &item{title: pr.Title, author: pr.Author, labels: pr.Labels, createdAt: pr.CreatedAt, state: pr.State,
		pull: &pullInfo{head: pr.HeadBranch, cross: pr.CrossRepository, closes: slices.Clone(pr.Closes)}}
	return g.add(pr.Number, it)
}

// AddComment adds a comment by author with body on the issue or pull
// request number and returns its id. It panics when there is no such issue
// or pull request.
func (g *GitHub) AddComment(number int, author, body string) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustItem(number)
	return g.comment(number, author, body).id
}

// SetLabels replaces the labels of the issue or pull request number with
// labels, creating those the repository lacks. It panics when there is no
// such issue or pull request.
func (g *GitHub) SetLabels(number int, labels ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustItem(number).labels = g.ensureLabels(labels)
	g.touch()
}

// SetState sets the state of the issue or pull request number, as closing,
// reopening or merging it does. It panics when there is no such issue or
// pull request.
func (g *GitHub) SetState(number int, state State) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mustItem(number).state = state
	g.touch()
}

// SetFile puts a file with content at path on the repository's default
// branch, as GitHub's contents API serves it.
func (g *GitHub) SetFile(path, content string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.files[path] = content
	g.touch()
}

// AddTeam adds the team slug to the organization org, with members' logins
// as its members. Adding a team under the repository's owner makes that
// owner an organization, as only organizations have teams.
func (g *GitHub) AddTeam(org, slug string, members ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.teams[org+"/"+slug] = slices.Clone(members)
	g.touch()
}

// Fail makes the next times gh calls matching call fail with the GitHub
// error status and message, as GitHub answers an HTTP error. call is a list
// of gh arguments separated by spaces, such as "issue edit 3" or "api
// --method PATCH"; a gh call matches when its arguments hold these words in
// this order, not necessarily next to each other. {owner} and {repo} stand
// for the repository's owner and name. A call the fake does not know stays
// a violation.
func (g *GitHub) Fail(call string, status int, message string, times int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	words := strings.Fields(g.resolve(call))
	g.failures = append(g.failures, &failure{words: words, status: status, message: message, left: times})
}

// CallLog returns every gh call Run received, in order, each as its quoted
// command line. It is for failure artifacts only: a test asserts on the
// repository's state, never on the calls that changed it.
func (g *GitHub) CallLog() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.log)
}
