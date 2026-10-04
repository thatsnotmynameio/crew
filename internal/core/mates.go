package core

import (
	"maps"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The short states of a mate's entry, by precedence after a startup reason
// (KTD6).
const (
	stateActing     = "acting"
	stateCannotAct  = "cannot act: "
	stateWritesLost = "writes as you"
	stateNotRenewed = "token not renewed"
)

// youName is the name of the boss's entry.
const youName = "you"

// MatesConfig is the mates crew acts as, as it found them at startup (KTD3).
type MatesConfig struct {
	// Default is the default mate, which acts for crew's own writes; empty
	// when the config names none.
	Default string
	// Names are the configured mates, the default first, in config order.
	Names []string
	// Unable holds, by name, the short reason of each mate that cannot act
	// at startup, such as "no key".
	Unable map[string]string
	// Login is the gh login crew acts as when it acts as the boss; empty
	// when unknown.
	Login string
}

// mates is what the model knows of the identities its actions and writes
// act as: the configured mates and the boss (KTD3).
type mates struct {
	config MatesConfig
	// writesLost is the warning of crew's writes as the default mate going
	// back to the boss; empty while they go as the default mate. Once set
	// it stays.
	writesLost string
	// notRenewed holds, by mate, the warning of its last failed renewal.
	notRenewed map[string]string
	// spent holds, by identity, what its ended actions spent this run; the
	// boss's is under "" (KTD4).
	spent map[string]crew.Spend
}

// WithMates gives the model the mates crew acts as (KTD3). Without it, the
// boss alone acts, and crew's writes go as the boss.
func WithMates(c MatesConfig) Option {
	return func(m *Model) {
		c.Names = slices.Clone(c.Names)
		c.Unable = maps.Clone(c.Unable)
		m.mates.config = c
	}
}

// acts reports whether name is a configured mate that acts at startup.
func (ms *mates) acts(name string) bool {
	_, unable := ms.config.Unable[name]
	return name != "" && !unable && slices.Contains(ms.config.Names, name)
}

// identity returns who an action of mate acts as: the mate when it acts at
// startup, the boss ("") otherwise (KTD4). A problem mid-run changes
// nothing: the action's session keeps the mate's identity.
func (ms *mates) identity(mate string) string {
	if ms.acts(mate) {
		return mate
	}
	return ""
}

// writer returns who crew's own writes go as: the default mate while it acts
// at startup and its writes have not fallen back, the boss ("") otherwise
// (KTD7).
func (ms *mates) writer() string {
	if ms.writesLost == "" {
		return ms.identity(ms.config.Default)
	}
	return ""
}

// credit adds spend to what identity's ended actions spent.
func (ms *mates) credit(identity string, spend crew.Spend) {
	if ms.spent == nil {
		ms.spent = map[string]crew.Spend{}
	}
	ms.spent[identity] = ms.spent[identity].Add(spend)
}

// state returns the short state of mate name, the first that holds (KTD6).
func (ms *mates) state(name string) string {
	if reason, ok := ms.config.Unable[name]; ok {
		return stateCannotAct + reason
	}
	if name == ms.config.Default && ms.writesLost != "" {
		return stateWritesLost
	}
	if _, ok := ms.notRenewed[name]; ok {
		return stateNotRenewed
	}
	return stateActing
}

// warnings returns the live warnings of mate name, in the order they arose:
// its writes', then its token's.
func (ms *mates) warnings(name string) []string {
	var out []string
	if name == ms.config.Default && ms.writesLost != "" {
		out = append(out, ms.writesLost)
	}
	if w, ok := ms.notRenewed[name]; ok {
		out = append(out, w)
	}
	return out
}

// matesChecked applies a reading of the mates' live state. Each mate that
// gains a problem emits MateStopped, and each whose state returns to acting
// emits MateActsAgain (KTD3). Mates that cannot act at startup and names not
// configured are ignored.
func (s *step) matesChecked(in MatesChecked) {
	ms := &s.m.mates
	for _, name := range ms.config.Names {
		if !ms.acts(name) {
			continue
		}
		before := ms.state(name)
		if name == ms.config.Default && in.WritesLost != "" && ms.writesLost == "" {
			ms.writesLost = in.WritesLost
			s.emit(MateStopped{At: s.at, Mate: name, Reason: stateWritesLost, Warning: in.WritesLost})
		}
		s.renewal(name, in.NotRenewed)
		if before != stateActing && ms.state(name) == stateActing {
			s.emit(MateActsAgain{At: s.at, Mate: name})
		}
	}
}

// renewal records the last renewal of mate name from notRenewed, and emits
// MateStopped when its token was renewed before and now is not.
func (s *step) renewal(name string, notRenewed map[string]string) {
	ms := &s.m.mates
	warning, failed := notRenewed[name]
	_, had := ms.notRenewed[name]
	switch {
	case failed:
		if !had {
			s.emit(MateStopped{At: s.at, Mate: name, Reason: stateNotRenewed, Warning: warning})
		}
		if ms.notRenewed == nil {
			ms.notRenewed = map[string]string{}
		}
		ms.notRenewed[name] = warning
	case had:
		delete(ms.notRenewed, name)
	}
}

// MateView is one identity crew acts as: a configured mate, or the boss.
type MateView struct {
	// Name is the mate's name; "you" for the boss's entry.
	Name string
	// You is set on the boss's entry.
	You bool
	// Login is the gh login crew acts as when it acts as the boss, on the
	// boss's entry; empty when unknown.
	Login string
	// Acting is set while the mate's state is "acting"; never on the boss's
	// entry.
	Acting bool
	// State is the mate's short state: "acting", "cannot act: <reason>",
	// "writes as you" or "token not renewed" (KTD6); empty on the boss's
	// entry.
	State string
	// ActsAsYou is set on a mate that cannot act at startup: its actions act
	// as the boss.
	ActsAsYou bool
	// Warnings are the mate's live warnings, in the order they arose: its
	// writes', then its token's.
	Warnings []string
	// Writes is set on the entry crew's own writes go as (KTD7).
	Writes bool
	// Pairs are the "stage/action" pairs, in workflow order, that act as the
	// entry. A mate that cannot act lists its own; the boss's entry lists
	// them too.
	Pairs []string
	// Running are the actions running as the entry now (KTD5), in the order
	// their issues were taken, then in their stage's action order.
	Running []RunningAction
	// Spend is what the entry's actions that ended this run spent; its
	// Sessions is how many acted (KTD4).
	Spend crew.Spend
}

// RunningAction is an action running as a mate or the boss.
type RunningAction struct {
	IssueRef string
	Stage    string
	Action   string
}

// matesView returns the configured mates' entries, in config order, then the
// boss's.
func (m *Model) matesView() []MateView {
	ms := &m.mates
	out := make([]MateView, 0, len(ms.config.Names)+1)
	for _, name := range ms.config.Names {
		state := ms.state(name)
		_, unable := ms.config.Unable[name]
		out = append(out, MateView{
			Name: name, Acting: state == stateActing, State: state, ActsAsYou: unable,
			Warnings: ms.warnings(name), Writes: ms.writer() == name,
			Pairs: m.pairs(func(mate string) bool { return mate == name }), Running: m.runningAs(name),
			Spend: ms.spent[name],
		})
	}
	return append(out, MateView{
		Name: youName, You: true, Login: ms.config.Login, Writes: ms.writer() == "",
		Pairs: m.pairs(func(mate string) bool { return ms.identity(mate) == "" }), Running: m.runningAs(""),
		Spend: ms.spent[""],
	})
}

// pairs returns the "stage/action" pairs, in workflow order, of the actions
// whose mate is.
func (m *Model) pairs(is func(mate string) bool) []string {
	var out []string
	for _, st := range m.stages {
		for _, a := range st.Actions {
			if is(a.Mate) {
				out = append(out, st.Name+"/"+a.Name)
			}
		}
	}
	return out
}

// runningAs returns the actions running as identity now: from their
// session's start until their spend lands (KTD5).
func (m *Model) runningAs(identity string) []RunningAction {
	var out []RunningAction
	for _, h := range m.issues {
		for _, a := range h.actions {
			running := a.phase == PhaseRunning || a.phase == PhaseChecking || a.phase == PhaseFinishing
			if running && m.mates.identity(a.mate) == identity {
				out = append(out, RunningAction{IssueRef: h.issue.Ref, Stage: m.stages[h.stage].Name, Action: a.name})
			}
		}
	}
	return out
}
