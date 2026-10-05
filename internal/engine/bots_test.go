package engine_test

import (
	"context"
	"maps"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// writesLost is the warning a tracker reports when crew's writes as ops go
// back to you.
const writesLost = "bot ops lost access to the repository: run `crew bots create ops` to install it; " +
	"crew writes as you until it restarts"

// notRenewed is the warning of developer's failed renewal.
const notRenewed = "bot developer could not renew its token: GitHub is down; its sessions and checks fail " +
	"once the current token expires, and crew tries again every minute"

// fallingBack is an acting fake tracker whose writes go back to you
// while it prepares, as the github tracker's do when creating labels fails
// as the writer.
type fallingBack struct {
	fake.ActingTracker
}

func (f fallingBack) Prepare(ctx context.Context, states []crew.State) error {
	f.SetWriterLost(writesLost)
	return f.ActingTracker.Prepare(ctx, states)
}

// renewals is a scripted engine.Config.BotFailures: what the bots' last
// renewals left failing, settable while the engine reads it.
type renewals struct {
	mu      sync.Mutex
	failing map[string]string
}

// set makes failing the bots' failed renewals.
func (r *renewals) set(failing map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failing = maps.Clone(failing)
}

// read implements engine.Config.BotFailures.
func (r *renewals) read() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return maps.Clone(r.failing)
}

// actingRig is an engine whose config names the bots ops, the default, and
// developer, with a latest-wins subscription of the test's own.
type actingRig struct {
	*rig

	latest <-chan engine.Update
}

// startActing starts an acting engine over tr, with failing as its renewal
// failures when not nil, and waits until every update so far is published.
func startActing(t *testing.T, tr port.Tracker, failing *renewals) *actingRig {
	t.Helper()
	cfg := config(t, tr, develop)
	cfg.ActAs, cfg.DefaultBot, cfg.Bots = true, "ops", []string{"ops", "developer"}
	if failing != nil {
		cfg.BotFailures = failing.read
	}
	e := engine.New(cfg)
	latest := e.SubscribeLatest()
	r := run(t, cfg, e)
	synctest.Wait()
	return &actingRig{rig: r, latest: latest}
}

// newest returns the update the latest-wins subscription holds, if any.
func (m *actingRig) newest() (engine.Update, bool) {
	select {
	case u := <-m.latest:
		return u, true
	default:
		return engine.Update{}, false
	}
}

// drained returns the updates the ordered queue holds, emptying it.
func (m *actingRig) drained() []engine.Update {
	var out []engine.Update
	for {
		select {
		case u := <-m.queue.Updates():
			out = append(out, u)
		default:
			return out
		}
	}
}

// settle empties both subscriptions.
func (m *actingRig) settle() {
	m.newest()
	m.drained()
}

// tick waits for the next said tick, when the engine reads the bots.
func tick() {
	time.Sleep(saidEvery)
	synctest.Wait()
}

// finish stops the engine and waits for Run.
func (m *actingRig) finish(t *testing.T) {
	t.Helper()
	m.engine.Stop()
	if _, err := m.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// botEntry returns the entry called name in u's snapshot.
func botEntry(t *testing.T, u engine.Update, name string) core.BotView {
	t.Helper()
	i := slices.IndexFunc(u.Snapshot.Bots, func(v core.BotView) bool { return v.Name == name })
	if i < 0 {
		t.Fatalf("Bots = %+v, want an entry called %s", u.Snapshot.Bots, name)
	}
	return u.Snapshot.Bots[i]
}

// botEvents returns the bot events in updates, in order.
func botEvents(updates []engine.Update) []core.Event {
	var out []core.Event
	for _, u := range updates {
		for _, e := range u.Events {
			switch e := e.(type) {
			case core.BotStopped:
				e.At = time.Time{}
				out = append(out, e)
			case core.BotActsAgain:
				e.At = time.Time{}
				out = append(out, e)
			}
		}
	}
	return out
}

// Covers AE4: crew's writes went back to you while the tracker
// prepared, before the core existed, and the first update says so.
func TestAE4AFallbackDuringPrepareShowsAtTheFirstUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := startActing(t, fallingBack{fake.NewActingTracker()}, nil)

		q := m.drained()
		if len(q) == 0 {
			t.Fatal("the ordered queue got no update")
		}
		want := []core.Event{core.BotStopped{Bot: "ops", Reason: "writes as you", Warning: writesLost}}
		if got := botEvents(q[:1]); !slices.Equal(got, want) {
			t.Errorf("the first update's bot events = %#v, want %#v", got, want)
		}
		if got := botEvents(q); len(got) != 1 {
			t.Errorf("the queue's bot events = %#v, want the one", got)
		}
		if ops := botEntry(t, q[0], "ops"); ops.State != "writes as you" || ops.Writes {
			t.Errorf("the first update's ops = %+v, want it writing as you", ops)
		}
		u, ok := m.newest()
		if !ok {
			t.Fatal("the latest subscription got no update")
		}
		if ops := botEntry(t, u, "ops"); ops.State != "writes as you" {
			t.Errorf("the latest ops = %+v, want it writing as you", ops)
		}
		if you := botEntry(t, u, "you"); !you.Writes {
			t.Errorf("the latest you = %+v, want crew's writes on it", you)
		}

		m.finish(t)
	})
}

func TestAFallbackMidRunShowsWithinOneSaidTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker()
		m := startActing(t, tr, nil)
		if u, ok := m.newest(); !ok || botEntry(t, u, "ops").State != "acting" {
			t.Fatalf("the latest update before the fallback = %+v, want ops acting", u)
		}
		m.settle()

		tr.SetWriterLost(writesLost)
		tick()

		want := []core.Event{core.BotStopped{Bot: "ops", Reason: "writes as you", Warning: writesLost}}
		u, ok := m.newest()
		if !ok {
			t.Fatal("the latest subscription got no update within one said tick")
		}
		if ops := botEntry(t, u, "ops"); ops.State != "writes as you" || !slices.Equal(ops.Warnings, []string{writesLost}) {
			t.Errorf("the latest ops = %+v, want it writing as you, with the warning", ops)
		}
		if got := botEvents([]engine.Update{u}); !slices.Equal(got, want) {
			t.Errorf("the latest update's bot events = %#v, want %#v", got, want)
		}
		if got := botEvents(m.drained()); !slices.Equal(got, want) {
			t.Errorf("the queue's bot events = %#v, want %#v", got, want)
		}

		m.finish(t)
	})
}

// Covers AE5: a bot whose renewal fails stops acting, and acts again once
// a renewal succeeds, each said once.
func TestAE5AFailedRenewalStopsTheBotAndASuccessfulOneMakesItActAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failing := &renewals{}
		m := startActing(t, fake.NewActingTracker(), failing)
		m.settle()

		failing.set(map[string]string{"developer": notRenewed})
		tick()
		u, ok := m.newest()
		if !ok || botEntry(t, u, "developer").State != "token not renewed" {
			t.Errorf("the latest update = %+v, want developer's token not renewed", u)
		}
		tick()
		failing.set(nil)
		tick()
		if u, ok := m.newest(); !ok || botEntry(t, u, "developer").State != "acting" {
			t.Errorf("the latest update = %+v, want developer acting again", u)
		}
		tick()

		want := []core.Event{
			core.BotStopped{Bot: "developer", Reason: "token not renewed", Warning: notRenewed},
			core.BotActsAgain{Bot: "developer"},
		}
		if got := botEvents(m.drained()); !slices.Equal(got, want) {
			t.Errorf("the queue's bot events = %#v, want %#v", got, want)
		}

		m.finish(t)
	})
}

func TestAnUnchangedReadingStepsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker()
		failing := &renewals{}
		m := startActing(t, tr, failing)
		tr.SetWriterLost(writesLost)
		failing.set(map[string]string{"developer": notRenewed})
		tick()
		if got := botEvents(m.drained()); len(got) != 2 {
			t.Fatalf("the queue's bot events = %#v, want ops's and developer's", got)
		}
		m.settle()

		for range 5 {
			tick()
		}

		if q := m.drained(); len(q) != 0 {
			t.Errorf("an unchanged reading queued %d updates, want none", len(q))
		}
		if u, ok := m.newest(); ok {
			t.Errorf("an unchanged reading published %+v, want nothing", u)
		}

		m.finish(t)
	})
}

func TestTheYouEntryCarriesTheTrackersLogin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker()
		tr.SetLogin("mguilarducci")
		m := startActing(t, tr, nil)

		u, ok := m.newest()
		if !ok {
			t.Fatal("the latest subscription got no update")
		}
		if you := botEntry(t, u, "you"); !you.You || you.Login != "mguilarducci" {
			t.Errorf("you = %+v, want the tracker's login", you)
		}
		names := make([]string, 0, len(u.Snapshot.Bots))
		for _, v := range u.Snapshot.Bots {
			names = append(names, v.Name)
		}
		if want := []string{"ops", "developer", "you"}; !slices.Equal(names, want) {
			t.Errorf("the entries = %q, want %q", names, want)
		}

		m.finish(t)
	})
}

func TestWithATrackerThatFindsNoLoginTheYouEntryHasNone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := startActing(t, fake.NewTracker(), nil)

		u, ok := m.newest()
		if !ok {
			t.Fatal("the latest subscription got no update")
		}
		if you := botEntry(t, u, "you"); you.Login != "" {
			t.Errorf("you = %+v, want no login", you)
		}
		if ops := botEntry(t, u, "ops"); ops.State != "acting" || !ops.Writes {
			t.Errorf("ops = %+v, want it acting, with crew's writes", ops)
		}

		m.finish(t)
	})
}

func TestABotThatCannotActAtStartupShowsItsReasonAndIgnoresItsReadings(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewActingTracker()
		cfg := config(t, tr, develop)
		cfg.ActAs, cfg.DefaultBot, cfg.Bots = true, "ops", []string{"ops"}
		cfg.Unable = map[string]string{"ops": "no key"}
		e := engine.New(cfg)
		latest := e.SubscribeLatest()
		m := &actingRig{rig: run(t, cfg, e), latest: latest}
		synctest.Wait()
		u, ok := m.newest()
		if !ok {
			t.Fatal("the latest subscription got no update")
		}
		if ops := botEntry(t, u, "ops"); ops.State != "cannot act: no key" || !ops.ActsAsYou {
			t.Errorf("ops = %+v, want it unable to act, with no key", ops)
		}

		tr.SetWriterLost(writesLost)
		tick()

		if got := botEvents(m.drained()); len(got) != 0 {
			t.Errorf("the queue's bot events = %#v, want none for a bot that cannot act", got)
		}

		m.finish(t)
	})
}
