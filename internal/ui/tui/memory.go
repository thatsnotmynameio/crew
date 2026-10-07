package tui

import (
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
)

// messageMemory is what the view remembers of each action across
// snapshots: its last message and branch. The engine's Said covers running
// sessions only, so the memory keeps them after the session ends, for as
// long as the action's issue has a card (R18; KTD9 of #151). The program
// owns one Model at a time, so the copies of a Model share it safely.
type messageMemory struct {
	actions map[actionKey]remembered
}

// actionKey is an action of an issue: its issue's key and its name.
type actionKey struct {
	issue  string
	action crew.ActionName
}

// remembered is an action's last message and branch, of its run that
// started at started; zero before it started.
type remembered struct {
	message, branch string
	started         time.Time
}

func newMessageMemory() *messageMemory {
	return &messageMemory{actions: map[actionKey]remembered{}}
}

// record remembers the branch of each held action and what each session
// last said, then forgets every issue with no card in cards (KTD9 of
// #151).
func (mm *messageMemory) record(snap engine.Snapshot, cards []card) {
	for _, iv := range snap.Issues {
		for _, a := range iv.Actions {
			mm.held(iv.Issue.Key, a)
		}
	}
	for _, s := range snap.Said {
		if text := clean(s.Text); text != "" {
			k := actionKey{s.IssueKey, s.Action}
			r := mm.actions[k]
			r.message = text
			mm.actions[k] = r
		}
	}
	mm.forget(cards)
}

// held remembers the branch of issue's held action a; a new run of it
// drops what its last run left, before its session starts too.
func (mm *messageMemory) held(issue string, a core.ActionView) {
	k := actionKey{issue, a.Name}
	r := mm.actions[k]
	if !a.Started.Equal(r.started) {
		r = remembered{started: a.Started}
	}
	if branch := clean(a.Branch); branch != "" {
		r.branch = branch
	}
	mm.actions[k] = r
}

// forget drops every issue with no card in cards.
func (mm *messageMemory) forget(cards []card) {
	carded := map[string]bool{}
	for _, c := range cards {
		carded[c.issue.Key] = true
	}
	for k := range mm.actions {
		if !carded[k.issue] {
			delete(mm.actions, k)
		}
	}
}

// last returns the last message and the branch the memory holds for
// issue's action, in that order, or empty strings for what it does not
// hold.
func (mm *messageMemory) last(issue string, action crew.ActionName) (string, string) {
	r := mm.actions[actionKey{issue, action}]
	return r.message, r.branch
}
