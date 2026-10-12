package fake

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guards.
var (
	_ port.StackFinder = (*Stacks)(nil)
	_ port.Tracker     = StackTracker{}
	_ port.StackFinder = StackTracker{}
)

// Stacks is a scriptable port.StackFinder, to embed in a fake tracker: it
// gives each issue the reading set for its key with SetStack, and the zero
// StackReading to the others, unless a failure scripted with FailStacks
// comes first. It records the issues of every read. Its zero value is
// ready to use.
type Stacks struct {
	mu       sync.Mutex
	readings map[string]crew.StackReading
	errs     []error
	reads    [][]crew.IssueID
}

// SetStack makes every later read give the issue with key reading.
func (s *Stacks) SetStack(key string, reading crew.StackReading) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readings == nil {
		s.readings = map[string]crew.StackReading{}
	}
	s.readings[key] = reading
}

// FailStacks makes the next len(errs) reads fail, in order, with errs.
func (s *Stacks) FailStacks(errs ...error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errs = append(s.errs, errs...)
}

// StackReads returns the IDs of the issues of each read received so far,
// failed ones included, in order.
func (s *Stacks) StackReads() [][]crew.IssueID {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]crew.IssueID, len(s.reads))
	for i, r := range s.reads {
		out[i] = slices.Clone(r)
	}
	return out
}

// FindStacks implements port.StackFinder.
func (s *Stacks) FindStacks(_ context.Context, issues []crew.Issue) ([]crew.Issue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]crew.IssueID, len(issues))
	for i, issue := range issues {
		ids[i] = issue.ID()
	}
	s.reads = append(s.reads, ids)
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		return nil, fmt.Errorf("read the stacks: %w", err)
	}
	out := make([]crew.Issue, len(issues))
	for i, issue := range issues {
		d := issue.Data()
		d.Stacking = s.readings[issue.ID().Key]
		out[i] = crew.NewIssue(d)
	}
	return out, nil
}

// StackTracker is a Tracker that also implements port.StackFinder, for the
// tests about stacking a blocked issue on its blocker's pull request. A
// plain *Tracker does not implement it.
type StackTracker struct {
	*Tracker
	*Stacks
}

// NewStackTracker returns a StackTracker holding issues, all open, which
// reads no stack until SetStack.
func NewStackTracker(issues ...crew.Issue) StackTracker {
	return StackTracker{Tracker: NewTracker(issues...), Stacks: &Stacks{}}
}
