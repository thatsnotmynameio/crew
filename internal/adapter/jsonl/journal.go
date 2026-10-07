// Package jsonl is the run journal's file adapter: one JSON line per rule
// run event, appended to a file under the repository's .crew/logs/ and
// never rewritten (KTD12). It writes and reads version 3 lines and skips
// the lines of earlier versions, whose runs start over.
package jsonl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/fileline"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guard.
var _ port.Journal = (*Journal)(nil)

// Journal is the run journal in one file. Only one goroutine calls it at a
// time, so its lines land in the order they were appended.
type Journal struct {
	root string
	path string
	run  string
}

// New returns the journal at path, repository-relative with slashes, in
// the repository whose root is root. run is the crew process's id, which
// every line it appends carries as run: the boss's cost record groups the
// ended lines by it.
func New(root, path, run string) *Journal {
	return &Journal{root: root, path: path, run: run}
}

// Load implements port.Journal: it returns the events of the journal's
// version 3 lines, in the order they were written, each issue in
// repository, as the journal belongs to one checkout. A missing journal
// holds none, and so does a file where its directory goes; the session
// logs that cannot be created there are reported as they fail. A line
// that does not parse, such as one cut short by a crash, of another
// version, such as the version 1 and 2 lines of earlier crews, or of a
// type this crew does not know, is skipped.
func (j *Journal) Load(repository crew.RepositoryID) ([]crew.RunEvent, error) {
	data, err := os.ReadFile(j.file())
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the run journal %s: %w", j.path, err)
	}
	var events []crew.RunEvent
	decoders := decoders()
	for text := range bytes.Lines(data) {
		var l line
		if err := json.Unmarshal(text, &l); err != nil {
			continue
		}
		if e, ok := l.event(decoders, repository); ok {
			events = append(events, e)
		}
	}
	return events, nil
}

// event returns the event l holds, its issue in repository, by decoders,
// or false when l is not a line this crew understands.
func (l line) event(decoders map[string]decoder, repository crew.RepositoryID) (crew.RunEvent, bool) {
	if l.Issue == "" || l.Version != version {
		return nil, false
	}
	return l.decode(decoders, l.eventHead(repository))
}

// Append implements port.Journal: it appends e as a line of its own,
// creating the journal and its directory as needed.
func (j *Journal) Append(e crew.RunEvent) error {
	data, err := json.Marshal(encode(e, j.run))
	if err != nil {
		return fmt.Errorf("encode a run event: %w", err)
	}
	f, err := j.open()
	if err != nil {
		return fmt.Errorf("open the run journal: %w", err)
	}
	err = fileline.Append(f, data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write the run journal: %w", err)
	}
	return nil
}

// file returns the journal's absolute path.
func (j *Journal) file() string {
	return filepath.Join(j.root, filepath.FromSlash(j.path))
}

// open opens the journal for reading and appending, creating it and its
// directory as needed.
func (j *Journal) open() (*os.File, error) {
	return fileline.Open(j.file())
}
