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
	"github.com/thatsnotmynameio/crew/internal/crew"
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

// journalLine is one line of the run journal. Its field names are a
// stable format, so other tools can read it. A
// value the harness did not report is left out, never written as zero, so
// its field is a pointer. Rule keeps the name stage, from before rules
// were called stages, so older journals still resume.
type journalLine struct {
	Version   int                `json:"v"`
	Event     string             `json:"event"`
	Time      time.Time          `json:"time"`
	Run       string             `json:"run,omitempty"`
	Issue     string             `json:"issue"`
	Ref       string             `json:"ref"`
	Rule      crew.RuleName      `json:"stage"`
	Action    crew.ActionName    `json:"action"`
	Workspace crew.WorkspaceName `json:"workspace"`
	Branch    string             `json:"branch"`
	Log       string             `json:"log"`
	// The fields below are set on ended lines only. DurationMS, from the
	// session's start to the action's end, is left out when no session
	// started, and so are the usage fields.
	Succeeded         *bool    `json:"succeeded,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	DurationMS        *int64   `json:"duration_ms,omitempty"`
	CostUSD           *float64 `json:"cost_usd,omitempty"`
	InputTokens       *int64   `json:"input_tokens,omitempty"`
	OutputTokens      *int64   `json:"output_tokens,omitempty"`
	CacheReadTokens   *int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens  *int64   `json:"cache_write_tokens,omitempty"`
	Turns             *int     `json:"turns,omitempty"`
	Models            []string `json:"models,omitempty"`
	PullRequest       string   `json:"pull_request,omitempty"`
	PullRequestURL    string   `json:"pull_request_url,omitempty"`
	PullRequestLookup string   `json:"pull_request_lookup,omitempty"`
}

// The values of a journal line's pull_request_lookup.
const (
	lookupFound       = "found"
	lookupNone        = "none"
	lookupNotLookedUp = "not looked up"
)

// readJournal returns the run journal's records in the order they were
// written, each issue in repository: the journal belongs to one checkout, so
// its lines hold only the issue's key. A missing journal holds none. A line
// that does not parse, such as one cut short by a crash, is skipped.
func (e *Engine) readJournal(repository crew.RepositoryID) ([]core.RunRecord, error) {
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
		if r, ok := l.record(repository); ok {
			records = append(records, r)
		}
	}
	return records, nil
}

// record returns the run record l holds, its issue in repository, or false
// when l is not a record this version of crew understands.
func (l journalLine) record(repository crew.RepositoryID) (core.RunRecord, bool) {
	if l.Version != journalVersion || l.Issue == "" || l.Workspace == "" {
		return core.RunRecord{}, false
	}
	r := core.RunRecord{
		At: l.Time, IssueID: crew.IssueID{Repository: repository, Key: l.Issue}, IssueRef: l.Ref,
		Rule: l.Rule, Action: l.Action, Workspace: l.Workspace, Branch: l.Branch, Log: l.Log,
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

// lineOf returns the journal line of r, in the crew run run.
func lineOf(r core.RunRecord, run string) journalLine {
	l := journalLine{
		Version: journalVersion, Event: eventStarted, Time: r.At.UTC(), Run: run, Issue: r.IssueID.Key,
		Ref: r.IssueRef, Rule: r.Rule, Action: r.Action, Workspace: r.Workspace, Branch: r.Branch, Log: r.Log,
	}
	if r.Event != core.RunEnded {
		return l
	}
	succeeded := r.Succeeded
	l.Event, l.Succeeded, l.Reason = eventEnded, &succeeded, r.Reason
	if !r.SessionStarted.IsZero() {
		l.DurationMS = new(r.At.Sub(r.SessionStarted).Milliseconds())
	}
	u := r.Usage
	if u.HasCost {
		l.CostUSD = new(u.Cost)
	}
	if u.HasTokens {
		l.InputTokens, l.OutputTokens = new(u.Tokens.Input), new(u.Tokens.Output)
		l.CacheReadTokens, l.CacheWriteTokens = new(u.Tokens.CacheRead), new(u.Tokens.CacheWrite)
	}
	if u.HasTurns {
		l.Turns = new(u.Turns)
	}
	l.Models = u.Models
	switch r.PullRequest.Lookup {
	case crew.PullRequestFound:
		l.PullRequestLookup, l.PullRequest, l.PullRequestURL = lookupFound, r.PullRequest.Ref, r.PullRequest.URL
	case crew.PullRequestNone:
		l.PullRequestLookup = lookupNone
	default:
		l.PullRequestLookup = lookupNotLookedUp
	}
	return l
}

// appendJournal appends r to the run journal, creating it and its directory
// as needed. Only the loop calls it, so lines land in the order the core
// asked for them (KTD3).
func (e *Engine) appendJournal(r core.RunRecord) error {
	data, err := json.Marshal(lineOf(r, e.run))
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
