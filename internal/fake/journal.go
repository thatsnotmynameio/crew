package fake

import (
	"slices"
	"sync"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guard.
var _ port.Journal = (*Journal)(nil)

// Journal is an in-memory run journal: it loads the events it was made
// with, then those appended since, and its appends fail once the test says
// so. It is safe for concurrent use.
// Its zero value holds no events and appends every one.
type Journal struct {
	mu       sync.Mutex
	past     []crew.RunEvent
	appended []crew.RunEvent
	failure  error
}

// NewJournal returns a journal holding past, as an earlier crew left it.
func NewJournal(past ...crew.RunEvent) *Journal {
	return &Journal{past: slices.Clone(past)}
}

// FailAppends makes every later Append fail with err, appending nothing;
// a nil err makes them append again.
func (j *Journal) FailAppends(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.failure = err
}

// Appended returns the events appended so far, in order.
func (j *Journal) Appended() []crew.RunEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.appended)
}

// Load implements port.Journal: the events the journal was made with, then
// those appended since, as they were given, whatever repository.
func (j *Journal) Load(crew.RepositoryID) ([]crew.RunEvent, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Concat(j.past, j.appended), nil
}

// Append implements port.Journal.
func (j *Journal) Append(e crew.RunEvent) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.failure != nil {
		return j.failure
	}
	j.appended = append(j.appended, e)
	return nil
}
