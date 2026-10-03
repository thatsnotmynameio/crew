package engine

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
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
)

// journalPath is the run journal, relative to the repository root: one JSON
// line per run start and end, appended and never rewritten (KTD1). It sits
// with the logs its lines point to, under a directory crew's ignore rules
// already cover.
const journalPath = logDir + "/runs.jsonl"

// journalVersion is the format of the journal's lines; a line of another
// version is skipped.
const journalVersion = 1

// The events a journal line records.
const (
	eventStarted = "started"
	eventEnded   = "ended"
)

// journalLine is one line of the run journal. Its field names are the
// documented format (docs/guide/crew.mdx), so other tools can read it.
type journalLine struct {
	Version   int       `json:"v"`
	Event     string    `json:"event"`
	Time      time.Time `json:"time"`
	Issue     string    `json:"issue"`
	Ref       string    `json:"ref"`
	Stage     string    `json:"stage"`
	Action    string    `json:"action"`
	Workspace string    `json:"workspace"`
	Branch    string    `json:"branch"`
	Log       string    `json:"log"`
	// Succeeded and Reason are set on ended lines only.
	Succeeded *bool  `json:"succeeded,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// readJournal returns the run journal's records in the order they were
// written. A missing journal holds none. A line that does not parse, such as
// one cut short by a crash, is skipped.
func (e *Engine) readJournal() ([]core.RunRecord, error) {
	data, err := os.ReadFile(filepath.Join(e.cfg.Root, filepath.FromSlash(journalPath)))
	// A file where the log directory goes is no journal either; the session
	// logs that cannot be created there are reported as they fail.
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the run journal %s: %w", journalPath, err)
	}
	var records []core.RunRecord
	for line := range bytes.Lines(data) {
		var l journalLine
		if err := json.Unmarshal(line, &l); err != nil {
			continue
		}
		if r, ok := l.record(); ok {
			records = append(records, r)
		}
	}
	return records, nil
}

// record returns the run record l holds, or false when l is not a record
// this version of crew understands.
func (l journalLine) record() (core.RunRecord, bool) {
	if l.Version != journalVersion || l.Issue == "" || l.Workspace == "" {
		return core.RunRecord{}, false
	}
	r := core.RunRecord{
		At: l.Time, IssueKey: l.Issue, IssueRef: l.Ref, Stage: l.Stage, Action: l.Action,
		Workspace: l.Workspace, Branch: l.Branch, Log: l.Log,
	}
	switch l.Event {
	case eventStarted:
		r.Event = core.RunStarted
	case eventEnded:
		if l.Succeeded == nil {
			return core.RunRecord{}, false
		}
		r.Event, r.Succeeded, r.Reason = core.RunEnded, *l.Succeeded, l.Reason
	default:
		return core.RunRecord{}, false
	}
	return r, true
}

// lineOf returns the journal line of r.
func lineOf(r core.RunRecord) journalLine {
	l := journalLine{
		Version: journalVersion, Event: eventStarted, Time: r.At.UTC(), Issue: r.IssueKey, Ref: r.IssueRef,
		Stage: r.Stage, Action: r.Action, Workspace: r.Workspace, Branch: r.Branch, Log: r.Log,
	}
	if r.Event == core.RunEnded {
		succeeded := r.Succeeded
		l.Event, l.Succeeded, l.Reason = eventEnded, &succeeded, r.Reason
	}
	return l
}

// appendJournal appends r to the run journal, creating it and its directory
// as needed. Only the loop calls it, so lines land in the order the core
// asked for them (KTD3).
func (e *Engine) appendJournal(r core.RunRecord) error {
	data, err := json.Marshal(lineOf(r))
	if err != nil {
		return fmt.Errorf("encode a run record: %w", err)
	}
	f, err := e.openAppend(journalPath)
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
