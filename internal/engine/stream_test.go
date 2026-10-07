package engine

import (
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/core"
)

func polled(n int) Update {
	return Update{Events: []core.Published{core.PollDone{Listed: n}}}
}

func listed(t *testing.T, u Update) int {
	t.Helper()
	if len(u.Events) != 1 {
		t.Fatalf("update has %d events, want 1", len(u.Events))
	}
	done, ok := u.Events[0].(core.PollDone)
	if !ok {
		t.Fatalf("update's event is %T, want core.PollDone", u.Events[0])
	}
	return done.Listed
}

// Under synctest, a publish that blocked would deadlock the bubble and fail
// the test rather than hang it.
func TestAQueueThatIsNeverReadNeverBlocksPublishingAndCountsItsDrops(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newStream()
		q := s.subscribeQueue(64)

		for i := range 10_000 {
			s.publish(polled(i))
		}

		if got, want := q.Dropped(), 10_000-64; got != want {
			t.Errorf("Dropped = %d, want %d", got, want)
		}
		s.close()
		var got []int
		for u := range q.Updates() {
			got = append(got, listed(t, u))
		}
		if len(got) != 64 || got[0] != 0 || got[63] != 63 {
			t.Errorf("queue delivered %d updates, %v…, want the first 64 in order", len(got), got[:min(len(got), 3)])
		}
	})
}

func TestTheLatestSubscriptionKeepsOnlyTheNewestUpdate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newStream()
		latest := s.subscribeLatest()

		for i := range 10_000 {
			s.publish(polled(i))
		}
		if got := listed(t, <-latest); got != 9_999 {
			t.Errorf("latest = update %d, want 9999", got)
		}
		s.publish(polled(10_000))
		s.close()

		var got []int
		for u := range latest {
			got = append(got, listed(t, u))
		}
		if len(got) != 1 || got[0] != 10_000 {
			t.Errorf("after close the subscription delivered %v, want [10000] then the end", got)
		}
	})
}
