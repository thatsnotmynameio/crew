// Package jsonl is the run journal's file adapter: one JSON line per rule
// run event, appended to a file under the repository's .crew/logs/ and
// never rewritten (KTD12). It writes version 2 lines and reads versions 1
// and 2, so a run that failed before crew wrote events still resumes.
package jsonl

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// Compile-time guard.
var _ port.Journal = (*Journal)(nil)

// The permissions of the journal's directory and file: the directory holds
// the session logs, which hold what sessions printed, so only you read
// them.
const (
	dirPerm  = 0o700
	filePerm = 0o600
)

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
// version 1 and 2 lines, in the order they were written, each issue in
// repository, as the journal belongs to one checkout. A missing journal
// holds none, and so does a file where its directory goes; the session
// logs that cannot be created there are reported as they fail. A line
// that does not parse, such as one cut short by a crash, of another
// version or of a type this crew does not know, is skipped.
func (j *Journal) Load(repository crew.RepositoryID) ([]crew.RunEvent, error) {
	data, err := os.ReadFile(j.file())
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the run journal %s: %w", j.path, err)
	}
	var events []crew.RunEvent
	for text := range bytes.Lines(data) {
		var l line
		if err := json.Unmarshal(text, &l); err != nil {
			continue
		}
		events = append(events, l.events(repository)...)
	}
	return events, nil
}

// events returns the events l holds, its issue in repository: none when l
// is not a line this crew understands.
func (l line) events(repository crew.RepositoryID) []crew.RunEvent {
	if l.Issue == "" {
		return nil
	}
	switch l.Version {
	case version1:
		if e, ok := l.v1Event(repository); ok {
			return []crew.RunEvent{e}
		}
	case version:
		if e, ok := l.decode(l.eventHead(repository)); ok {
			return []crew.RunEvent{e}
		}
	}
	return nil
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
	err = appendLine(f, data)
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
	path := j.file()
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, fmt.Errorf("create the log directory: %w", err)
	}
	//nolint:gosec // crew builds the path under .crew/logs
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, filePerm)
	if err != nil {
		return nil, fmt.Errorf("open for appending: %w", err)
	}
	return f, nil
}

// appendLine appends data to f, a file opened for reading and appending, as
// a line of its own.
func appendLine(f *os.File, data []byte) error {
	if err := startLine(f); err != nil {
		return err
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append the line: %w", err)
	}
	return nil
}

// startLine makes the next write to f, a file opened for reading and
// appending, start on a line of its own: when f ends in the middle of a
// line, as after a crash during a write, it writes a newline first.
func startLine(f *os.File) error {
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("find the end of the last line: %w", err)
	}
	if info.Size() == 0 {
		return nil
	}
	last := make([]byte, 1)
	if _, err := f.ReadAt(last, info.Size()-1); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("find the end of the last line: %w", err)
	}
	if last[0] == '\n' {
		return nil
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("end the last line: %w", err)
	}
	return nil
}
