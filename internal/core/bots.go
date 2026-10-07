package core

import (
	"maps"
	"slices"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The short states of a bot's entry, by precedence after a startup reason
// (KTD6).
const (
	stateActing     = "acting"
	stateCannotAct  = "cannot act: "
	stateWritesLost = "writes as you"
	stateNotRenewed = "token not renewed"
)

// youName is the name of the "you" entry.
const youName = "you"

// BotsConfig is the bots crew acts as, as it found them at startup (KTD3).
type BotsConfig struct {
	// Default is the default bot, which acts for crew's own writes; empty
	// when the config names none.
	Default crew.BotName
	// Names are the configured bots, the default first, in config order.
	Names []crew.BotName
	// Unable holds, by name, the short reason of each bot that cannot act
	// at startup, such as "no key".
	Unable map[crew.BotName]string
	// Login is the gh login crew acts as when it acts as you; empty
	// when unknown.
	Login string
	// Logins holds, by name, the login each bot that acts at startup acts
	// as, such as crew-developer[bot] (KTD-W8).
	Logins map[crew.BotName]string
}

// bots is what the model knows of the identities its actions and writes
// act as: the configured bots and you (KTD3).
type bots struct {
	config BotsConfig
	// writesLost is the warning of crew's writes as the default bot going
	// back to you; empty while they go as the default bot. Once set it
	// stays.
	writesLost string
	// notRenewed holds, by bot, the warning of its last failed renewal.
	notRenewed map[crew.BotName]string
	// spent holds, by identity, what its ended actions spent this run; yours
	// is under "" (KTD4).
	spent map[crew.BotName]crew.Spend
}

// WithBots gives the model the bots crew acts as (KTD3). Without it, you
// alone act, and crew's writes go as you.
func WithBots(c BotsConfig) Option {
	return func(m *Model) {
		c.Names = slices.Clone(c.Names)
		c.Unable = maps.Clone(c.Unable)
		c.Logins = maps.Clone(c.Logins)
		m.bots.config = c
	}
}

// acts reports whether name is a configured bot that acts at startup.
func (ms *bots) acts(name crew.BotName) bool {
	_, unable := ms.config.Unable[name]
	return name != "" && !unable && slices.Contains(ms.config.Names, name)
}

// identity returns who an action of bot acts as: the bot when it acts at
// startup, you ("") otherwise (KTD4). A problem mid-run changes
// nothing: the action's session keeps the bot's identity.
func (ms *bots) identity(bot crew.BotName) crew.BotName {
	if ms.acts(bot) {
		return bot
	}
	return ""
}

// login returns the login a session of bot acts as: its bot's when the bot
// acts at startup, else yours, the gh login; empty when unknown (KTD-W8).
func (ms *bots) login(bot crew.BotName) string {
	if ms.acts(bot) {
		return ms.config.Logins[bot]
	}
	return ms.config.Login
}

// writer returns who crew's own writes go as: the default bot while it acts
// at startup and its writes have not fallen back, you ("") otherwise
// (KTD7).
func (ms *bots) writer() crew.BotName {
	if ms.writesLost == "" {
		return ms.identity(ms.config.Default)
	}
	return ""
}

// credit adds spend to what identity's ended actions spent.
func (ms *bots) credit(identity crew.BotName, spend crew.Spend) {
	if ms.spent == nil {
		ms.spent = map[crew.BotName]crew.Spend{}
	}
	ms.spent[identity] = ms.spent[identity].Add(spend)
}

// state returns the short state of bot name, the first that holds (KTD6).
func (ms *bots) state(name crew.BotName) string {
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

// warnings returns the live warnings of bot name, in the order they arose:
// its writes', then its token's.
func (ms *bots) warnings(name crew.BotName) []string {
	var out []string
	if name == ms.config.Default && ms.writesLost != "" {
		out = append(out, ms.writesLost)
	}
	if w, ok := ms.notRenewed[name]; ok {
		out = append(out, w)
	}
	return out
}

// botsChecked applies a reading of the bots' live state. Each bot that
// gains a problem emits BotStopped, and each whose state returns to acting
// emits BotActsAgain (KTD3). Bots that cannot act at startup and names not
// configured are ignored.
func (s *step) botsChecked(in BotsChecked) {
	ms := &s.m.bots
	for _, name := range ms.config.Names {
		if !ms.acts(name) {
			continue
		}
		before := ms.state(name)
		if name == ms.config.Default && in.WritesLost != "" && ms.writesLost == "" {
			ms.writesLost = in.WritesLost
			s.emit(BotStopped{At: s.at, Bot: name, Reason: stateWritesLost, Warning: in.WritesLost})
		}
		s.renewal(name, in.NotRenewed)
		if before != stateActing && ms.state(name) == stateActing {
			s.emit(BotActsAgain{At: s.at, Bot: name})
		}
	}
}

// renewal records the last renewal of bot name from notRenewed, and emits
// BotStopped when its token was renewed before and now is not.
func (s *step) renewal(name crew.BotName, notRenewed map[crew.BotName]string) {
	ms := &s.m.bots
	warning, failed := notRenewed[name]
	_, had := ms.notRenewed[name]
	switch {
	case failed:
		if !had {
			s.emit(BotStopped{At: s.at, Bot: name, Reason: stateNotRenewed, Warning: warning})
		}
		if ms.notRenewed == nil {
			ms.notRenewed = map[crew.BotName]string{}
		}
		ms.notRenewed[name] = warning
	case had:
		delete(ms.notRenewed, name)
	}
}

// BotView is one identity crew acts as: a configured bot, or you.
type BotView struct {
	// Name is the bot's name; "you" for the "you" entry.
	Name crew.BotName
	// You is set on the "you" entry.
	You bool
	// Login is the gh login crew acts as when it acts as you, on the
	// "you" entry; empty when unknown.
	Login string
	// Acting is set while the bot's state is "acting"; never on your
	// entry.
	Acting bool
	// State is the bot's short state: "acting", "cannot act: <reason>",
	// "writes as you" or "token not renewed" (KTD6); empty on your
	// entry.
	State string
	// ActsAsYou is set on a bot that cannot act at startup: its actions act
	// as you.
	ActsAsYou bool
	// Warnings are the bot's live warnings, in the order they arose: its
	// writes', then its token's.
	Warnings []string
	// Writes is set on the entry crew's own writes go as (KTD7).
	Writes bool
	// Pairs are the "rule/action" pairs, in rule order, that act as the
	// entry. A bot that cannot act lists its own; the "you" entry lists
	// them too.
	Pairs []string
	// Running are the actions running as the entry now (KTD5), in the order
	// their issues were taken, then in their rule's action order.
	Running []RunningAction
	// Spend is what the entry's actions that ended this run spent; its
	// Sessions is how many acted (KTD4).
	Spend crew.Spend
}

// RunningAction is an action running as a bot or you.
type RunningAction struct {
	IssueRef string
	Rule     crew.RuleName
	Action   crew.ActionName
}

// botsView returns the configured bots' entries, in config order, then the
// "you" entry.
func (m *Model) botsView() []BotView {
	ms := &m.bots
	out := make([]BotView, 0, len(ms.config.Names)+1)
	for _, name := range ms.config.Names {
		state := ms.state(name)
		_, unable := ms.config.Unable[name]
		out = append(out, BotView{
			Name: name, Acting: state == stateActing, State: state, ActsAsYou: unable,
			Warnings: ms.warnings(name), Writes: ms.writer() == name,
			Pairs: m.pairs(func(bot crew.BotName) bool { return bot == name }), Running: m.runningAs(name),
			Spend: ms.spent[name],
		})
	}
	return append(out, BotView{
		Name: youName, You: true, Login: ms.config.Login, Writes: ms.writer() == "",
		Pairs: m.pairs(func(bot crew.BotName) bool { return ms.identity(bot) == "" }), Running: m.runningAs(""),
		Spend: ms.spent[""],
	})
}

// pairs returns the "rule/action" pairs, in rule order, of the session
// actions whose bot is. A shell action has no bot of its own: it acts as
// its run's latest session (KTD13).
func (m *Model) pairs(is func(bot crew.BotName) bool) []string {
	var out []string
	for _, rule := range m.rules {
		for _, a := range rule.Actions {
			if spec, ok := a.Kind.(crew.SessionSpec); ok && is(spec.Bot.Name) {
				out = append(out, string(rule.Name)+"/"+string(a.Name))
			}
		}
	}
	return out
}

// runningAs returns the actions running as identity now: each session from
// its start until it ends, and each shell or function action while its
// script or function runs, as its run's latest session, or as you before
// any (KTD5, KTD13, KTD-S11).
func (m *Model) runningAs(identity crew.BotName) []RunningAction {
	var out []RunningAction
	for _, h := range m.issues {
		if m.bots.identity(h.run.Bot().Name) != identity {
			continue
		}
		for _, a := range h.run.Actions() {
			if spending(a.State()) {
				out = append(out, RunningAction{IssueRef: h.run.Issue().Ref(), Rule: h.run.Rule(), Action: a.Name()})
			}
		}
	}
	return out
}

// spending reports whether an action run in state acts on the tracker now:
// its session, its script or its function runs. The run's latest session
// is then the running session, so the run's bot is the action's.
func spending(state crew.ActionRunState) bool {
	switch state.(type) {
	case crew.InSession, crew.InShell, crew.InFunction:
		return true
	case crew.AwaitingTurn, crew.DoneInEarlierRun, crew.StartingSession, crew.Finished, crew.NotRun:
	}
	return false
}
