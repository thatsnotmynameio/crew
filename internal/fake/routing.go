package fake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guards: the routing tracker implements the tracker
// capabilities a rule's routes use.
var (
	_ port.Commenter      = (*Routing)(nil)
	_ port.CommentLister  = (*Routing)(nil)
	_ port.Delegator      = (*Routing)(nil)
	_ port.LoginFinder    = (*Routing)(nil)
	_ port.Tracker        = RoutingTracker{}
	_ port.Preparer       = RoutingTracker{}
	_ port.StatusReporter = RoutingTracker{}
	_ port.Commenter      = RoutingTracker{}
	_ port.Closer         = RoutingTracker{}
	_ port.CommentLister  = RoutingTracker{}
	_ port.Delegator      = RoutingTracker{}
	_ port.LoginFinder    = RoutingTracker{}
)

// Comment is a comment the fake tracker posted on the issue with Key.
type Comment struct {
	Key  string
	Body string
}

// Closing is a close the fake tracker applied to the issue with Key, which
// was in From.
type Closing struct {
	Key  string
	From crew.State
}

// Routing is a scriptable port.Commenter, port.CommentLister,
// port.Delegator and port.LoginFinder, and the record of a RoutingTracker's
// closes, to embed in a RoutingTracker. It records each comment and
// delegation posted and serves the comments SetComments scripts, then those
// it posted, unless a failure scripted with FailComments, FailClosings or
// FailCommentLists comes first. Its zero value is ready to use: it posts as
// no login and finds none.
type Routing struct {
	mu        sync.Mutex
	postErrs  failures
	closeErrs failures
	listErrs  failures
	posted    []Comment
	delegated []crew.Delegation
	closings  []Closing
	listings  map[string][]crew.Comment
	lists     map[string]int
	writer    string
	login     string
}

// Comment implements port.Commenter. It records body, as given, on the
// issue unless a scripted failure comes first, and lists it among the
// issue's comments (list).
func (r *Routing) Comment(_ context.Context, id crew.IssueID, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.postErrs.pop(id.Key); err != nil {
		return fmt.Errorf("comment on issue %s: %w", id.Key, err)
	}
	r.posted = append(r.posted, Comment{Key: id.Key, Body: body})
	r.list(id.Key, body)
	return nil
}

// Delegate implements port.Delegator. It records delegation, as given, and
// lists a comment that mentions its answerer with the delegation's marker
// among the issue's comments (list).
func (r *Routing) Delegate(_ context.Context, delegation crew.Delegation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.delegated = append(r.delegated, delegation)
	r.list(delegation.IssueID.Key, "@"+delegation.Answerer+"\n\n"+crew.DelegatedMarker(delegation.ID)+"\n")
	return nil
}

// SetWriter sets the login the comments and delegations posted from now on
// are written as.
func (r *Routing) SetWriter(login string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writer = login
}

// Login implements port.LoginFinder: it returns what SetLogin last set.
func (r *Routing) Login() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.login
}

// SetLogin sets the login Login returns.
func (r *Routing) SetLogin(login string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.login = login
}

// Delegations returns the delegations posted so far, in order.
func (r *Routing) Delegations() []crew.Delegation {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.delegated)
}

// Comments implements port.CommentLister: the comments SetComments last
// set for the issue, then those posted on it since, none without, unless a
// scripted failure comes first. It counts each listing, failed or not.
func (r *Routing) Comments(_ context.Context, id crew.IssueID) ([]crew.Comment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lists == nil {
		r.lists = map[string]int{}
	}
	r.lists[id.Key]++
	if err := r.listErrs.pop(id.Key); err != nil {
		return nil, fmt.Errorf("list the comments of issue %s: %w", id.Key, err)
	}
	return slices.Clone(r.listings[id.Key]), nil
}

// SetComments sets the comments Comments lists for the issue with key,
// oldest first, in place of those listed before, the posted ones included.
func (r *Routing) SetComments(key string, comments ...crew.Comment) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.listings == nil {
		r.listings = map[string][]crew.Comment{}
	}
	r.listings[key] = slices.Clone(comments)
}

// CommentLists returns how many times Comments listed the comments of the
// issue with key, failed listings included.
func (r *Routing) CommentLists(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lists[key]
}

// FailComments makes the next len(errs) comments posted on the issue with
// key fail, in order, with errs. Wrap port.ErrRefused or
// port.ErrMovedMeanwhile for those classes; any other error is transient.
func (r *Routing) FailComments(key string, errs ...error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.postErrs.add(key, errs...)
}

// FailClosings makes the next len(errs) closes of the issue with key fail,
// as FailComments does for comments.
func (r *Routing) FailClosings(key string, errs ...error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closeErrs.add(key, errs...)
}

// FailCommentLists makes the next len(errs) comment listings of the issue
// with key fail, as FailComments does for comments.
func (r *Routing) FailCommentLists(key string, errs ...error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listErrs.add(key, errs...)
}

// Posted returns the comments posted so far, in order. Failed comments are
// not among them.
func (r *Routing) Posted() []Comment {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.posted)
}

// Closings returns the closes applied so far, in order. Failed closes, and
// those that found nothing to change, are not among them.
func (r *Routing) Closings() []Closing {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.closings)
}

// list adds body, with crew's marker on its own last line after a blank
// line, as the github adapter posts it, to the comments of the issue with
// key, written as the writer SetWriter set. The caller holds mu.
func (r *Routing) list(key, body string) {
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if r.listings == nil {
		r.listings = map[string][]crew.Comment{}
	}
	r.listings[key] = append(r.listings[key], crew.Comment{Author: r.writer, Body: body + "\n" + crew.PostedMarker + "\n"})
}

// RoutingTracker is a ReportingTracker that also implements port.Commenter,
// port.Closer, port.CommentLister, port.Delegator and port.LoginFinder, for
// the tests about a rule's routes. A plain *Tracker or ReportingTracker does not implement
// them.
type RoutingTracker struct {
	ReportingTracker
	*Routing
}

// NewRoutingTracker returns a RoutingTracker holding issues, all open,
// whose Prepare, status writes, comments, closes, comment listings and
// delegations succeed until told otherwise, and which lists no comment until
// SetComments or a post, and finds no login until SetLogin.
func NewRoutingTracker(issues ...crew.Issue) RoutingTracker {
	return RoutingTracker{ReportingTracker: NewReportingTracker(issues...), Routing: &Routing{}}
}

// Close implements port.Closer as the github adapter does. A scripted
// failure comes first; then an unknown issue, an open one not in from and a
// closed one in crew states but not from are ErrMovedMeanwhile. Otherwise
// it closes the issue, so List no longer returns it, and takes its crew
// states off, leaving its other labels; it records the close unless the
// issue was already closed in no crew state.
func (r RoutingTracker) Close(_ context.Context, id crew.IssueID, from crew.State) error {
	r.mu.Lock() // the Routing's
	defer r.mu.Unlock()
	if err := r.closeErrs.pop(id.Key); err != nil {
		return fmt.Errorf("close issue %s from %s: %w", id.Key, from, err)
	}
	r.Tracker.mu.Lock()
	defer r.Tracker.mu.Unlock()
	moved := fmt.Errorf("close issue %s from %s: %w", id.Key, from, port.ErrMovedMeanwhile)
	ti := r.find(id.Key)
	if ti == nil {
		return moved
	}
	states := ti.issue.States()
	if !slices.Contains(states, from) && (!ti.closed || len(states) > 0) {
		return moved
	}
	if ti.closed && len(states) == 0 {
		return nil
	}
	ti.closed = true
	ti.setStates(nil)
	r.closings = append(r.closings, Closing{Key: id.Key, From: from})
	return nil
}
