package engine_test

import (
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
)

// saidEvery is how often the engine refreshes what the sessions last said.
const saidEvery = 2 * time.Second

// narrating is an engine running issue 1's development session, which
// narrates, with a latest-wins subscription of the test's own.
type narrating struct {
	*rig
	cfg     engine.Config
	latest  <-chan engine.Update
	session *fake.Session
}

// startNarrating starts a narrating engine, waits until its session runs
// and every update so far is published, then empties both subscriptions.
func startNarrating(t *testing.T) *narrating {
	t.Helper()
	cfg := config(t, fake.NewTracker(issue(1, ready)), develop)
	cfg.Harness = fake.NewNarratingHarness()
	e := engine.New(cfg)
	latest := e.SubscribeLatest()
	r := run(t, cfg, e)
	s := r.sessions(1)["issue-1-development"]
	synctest.Wait()
	n := &narrating{rig: r, cfg: cfg, latest: latest, session: s}
	if _, ok := n.next(); !ok {
		t.Fatal("starting the session published no update")
	}
	n.queued()
	return n
}

// next returns the update the latest-wins subscription holds, if any.
func (n *narrating) next() (engine.Update, bool) {
	select {
	case u := <-n.latest:
		return u, true
	default:
		return engine.Update{}, false
	}
}

// queued returns the updates the ordered queue holds, emptying it.
func (n *narrating) queued() []engine.Update {
	var out []engine.Update
	for {
		select {
		case u := <-n.queue.Updates():
			out = append(out, u)
		default:
			return out
		}
	}
}

// finish stops the engine and waits for Run.
func (n *narrating) finish(t *testing.T) {
	t.Helper()
	n.engine.Stop()
	if _, err := n.wait(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// refreshed waits for the next said refresh and returns the update the
// latest-wins subscription then holds, failing without one.
func (n *narrating) refreshed(t *testing.T) engine.Update {
	t.Helper()
	time.Sleep(saidEvery)
	synctest.Wait()
	u, ok := n.next()
	if !ok {
		t.Fatal("the latest subscription got no update on the said refresh")
	}
	return u
}

func TestR18TheLatestSubscriberGetsTheSessionsScrubbedWordsBeforeAnyPoll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := startNarrating(t)
		begun := time.Now()

		n.session.Say("Pushed with ghp_abc123XYZ from " + n.cfg.Root + "/internal/core in " + n.cfg.Home)
		u := n.refreshed(t)

		want := []core.Said{{
			IssueKey: "1", Action: "development", Text: "Pushed with [redacted token] from ./internal/core in ~",
		}}
		if !reflect.DeepEqual(u.Snapshot.Said, want) {
			t.Errorf("Said = %#v, want %#v", u.Snapshot.Said, want)
		}
		if len(u.Events) != 0 {
			t.Errorf("the said refresh's events = %#v, want none", u.Events)
		}
		if waited := time.Since(begun); waited > saidEvery {
			t.Errorf("the words took %v, want at most %v, before the poll at %v", waited, saidEvery, poll)
		}
		if q := n.queued(); len(q) != 0 {
			t.Errorf("the ordered queue got %d updates on the said refresh, want none", len(q))
		}

		n.finish(t)
	})
}

func TestR18UnchangedWordsPublishNothingOnTheNextRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := startNarrating(t)
		n.session.Say("Running the tests")
		n.refreshed(t)

		time.Sleep(saidEvery)
		synctest.Wait()
		if u, ok := n.next(); ok {
			t.Errorf("unchanged words published %#v, want nothing", u)
		}

		n.finish(t)
	})
}

func TestR18AStepAfterARefreshCarriesTheSameWords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := startNarrating(t)
		n.session.Say("Running the tests")
		said := n.refreshed(t).Snapshot.Said
		if len(said) != 1 {
			t.Fatalf("Said = %#v, want the session's words", said)
		}

		time.Sleep(poll - saidEvery)
		synctest.Wait()

		q := n.queued()
		if len(q) == 0 {
			t.Fatal("the poll published no update to the ordered queue")
		}
		last := q[len(q)-1]
		polled := slices.ContainsFunc(last.Events, func(e core.Event) bool {
			_, ok := e.(core.PollDone)
			return ok
		})
		if !polled || !reflect.DeepEqual(last.Snapshot.Said, said) {
			t.Errorf("the poll's update = %#v, want a PollDone carrying Said %#v", last, said)
		}

		n.finish(t)
	})
}

func TestR18OnceTheSessionEndsTheNextRefreshDropsItsWords(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		n := startNarrating(t)
		n.session.Say("Done")
		if said := n.refreshed(t).Snapshot.Said; len(said) != 1 {
			t.Fatalf("Said = %#v, want the session's words", said)
		}

		n.session.End(crew.Outcome{Succeeded: true, Reason: "done"})
		if said := n.refreshed(t).Snapshot.Said; len(said) != 0 {
			t.Errorf("Said = %#v after the session ended, want nothing", said)
		}

		n.finish(t)
	})
}

func TestR18SessionsThatDoNotNarratePublishNothingOnTheRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := config(t, fake.NewTracker(issue(1, ready)), develop)
		e := engine.New(cfg)
		latest := e.SubscribeLatest()
		r := run(t, cfg, e)
		r.sessions(1)
		synctest.Wait()
		<-latest

		time.Sleep(10 * saidEvery)
		synctest.Wait()
		select {
		case u := <-latest:
			t.Errorf("a session that cannot narrate published %#v, want nothing", u)
		default:
		}

		r.engine.Stop()
		if _, err := r.wait(); err != nil {
			t.Fatalf("Run: %v", err)
		}
	})
}
