package core

import (
	"slices"
	"time"
	"uuid"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// statistics is what the model knows of the crew process it records in
// the statistics store, when it records statistics (KTD4).
type statistics struct {
	version string
	folder  string
	tracker crew.TrackerName
	// process is the id of the recorded process, which the work it runs
	// points to; empty until Started.
	process crew.ProcessID
	// states holds, by kind of item, the states the rules taking that kind
	// name: the labels that count an item of that kind (KTD1).
	states map[crew.Kind][]crew.State
	// issues holds, by item id, each item recorded in this process.
	issues map[crew.IssueID]recordedIssue
}

// recordedIssue is what the model knows of an item it recorded (KTD4).
type recordedIssue struct {
	// state is the last crew state the item was known at; empty when it
	// was first seen in two or more.
	state crew.State
	// landed is the listing generation when a move crew made of the item
	// last landed: a listing of that generation may predate it (KTD5).
	landed int
}

// newStatistics returns what a model of rules knows of the process of crew
// version, working in the repository at folder on tracker, before it
// starts.
func newStatistics(rules []crew.Rule, version, folder string, tracker crew.TrackerName) *statistics {
	states := map[crew.Kind][]crew.State{}
	for _, kind := range []crew.Kind{crew.KindIssue, crew.KindPullRequest} {
		takers := slices.DeleteFunc(slices.Clone(rules), func(r crew.Rule) bool { return r.Takes != kind })
		states[kind] = crew.RuleStates(takers)
	}
	return &statistics{
		version: version, folder: folder, tracker: tracker, states: states, issues: map[crew.IssueID]recordedIssue{},
	}
}

// statisticsInput applies in, an input about the statistics store.
func (s *step) statisticsInput(in statisticsInput) {
	switch in := in.(type) {
	case Started:
		s.started(in.Seed, in.Repository)
	case StatisticFailed:
		s.emit(StatisticNotRecorded{At: s.at, Statistic: in.Statistic, Reason: in.Reason})
	}
}

// started records the process, with an id minted from seed, then
// repository, the one it works in, once, when the model records statistics
// (KTD3).
func (s *step) started(seed uuid.UUID, repository crew.Repository) {
	st := s.m.statistics
	if st == nil || st.process != "" {
		return
	}
	st.process = crew.ProcessID(seed.String())
	s.command(RecordStatistic{Statistic: crew.Process{
		ID: st.process, Version: st.version, Folder: st.folder, Start: s.at,
	}})
	s.command(RecordStatistic{Statistic: crew.RepositoryRecord{Tracker: st.tracker, Repository: repository}})
}

// sighted records each counted item of a listing's answer the first time
// this process finds it, and each change of its one crew state since,
// made outside crew at the listing's time (KTD4). It compares no item crew
// holds, nor one whose move crew made landed while this listing was
// outstanding, which may predate it: their moves are the runs' to record
// (KTD5). Only one listing is outstanding at a time, so this one is
// generation m.listings.
func (s *step) sighted(issues []crew.Issue) {
	m := s.m
	st := m.statistics
	if st == nil {
		return
	}
	for _, issue := range issues {
		if !st.counts(issue) {
			continue
		}
		known, ok := st.issues[issue.ID()]
		if !ok {
			s.sight(issue)
			continue
		}
		state, alone := issue.OnlyState()
		if !alone || m.held(issue.ID()) != nil || known.landed >= m.listings {
			continue
		}
		if known.state == "" {
			known.state = state
			st.issues[issue.ID()] = known
			continue
		}
		s.moved(issue.ID(), known.state, state, s.at, crew.Optional[crew.RuleRunID]{})
	}
}

// counts reports whether issue is in a state a rule of its kind names
// (KTD1).
func (st *statistics) counts(issue crew.Issue) bool {
	return slices.ContainsFunc(issue.States(), func(state crew.State) bool {
		return slices.Contains(st.states[issue.Kind()], state)
	})
}

// sight records issue, which this process finds for the first time, with
// the tracker's creation date when it gives one and its one state when it
// is in one, which becomes its last state (R7).
func (s *step) sight(issue crew.Issue) {
	st := s.m.statistics
	sighting := crew.IssueSighting{
		Tracker: st.tracker, Issue: issue.ID(), Ref: issue.Ref(), Kind: issue.Kind(), Seen: s.at,
	}
	if created := issue.Created(); !created.IsZero() {
		sighting.Created = crew.Some(created)
	}
	state, alone := issue.OnlyState()
	if alone {
		sighting.State = crew.Some(state)
	}
	st.issues[issue.ID()] = recordedIssue{state: state}
	s.command(RecordStatistic{Statistic: sighting})
}

// moved records the move of the item identified by id from one crew state
// to another, which crew made or saw at seen, made by run or outside crew
// when run is none, and keeps to as the item's last state (KTD4). It
// records no move that leaves or reaches no state, or stays in one. A move
// crew made keeps the listing generation it landed at (KTD5).
func (s *step) moved(id crew.IssueID, from, to crew.State, seen time.Time, run crew.Optional[crew.RuleRunID]) {
	st := s.m.statistics
	if st == nil || from == "" || to == "" || from == to {
		return
	}
	known := st.issues[id]
	known.state = to
	if _, byRun := run.Get(); byRun {
		known.landed = s.m.listings
	}
	st.issues[id] = known
	s.command(RecordStatistic{Statistic: crew.LabelMove{
		Tracker: st.tracker, Issue: id, From: from, To: to, Seen: seen, Run: run,
	}})
}
