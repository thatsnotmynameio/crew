package core_test

import (
	"slices"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// The triage stage's states in these tests, as label text.
const (
	needsTriage crew.State = "needs triage"
	triaging    crew.State = "triaging"
)

// queued returns a workflow of two one-action stages: triage, in the queue
// triage, then development, in the queue development.
func queued(triage, development crew.Queue) []crew.Stage {
	return []crew.Stage{
		{
			Name: "triage", Label: needsTriage, MovesTo: triaging, OnSuccess: ready, OnFailure: needsAttention,
			Actions: []crew.Action{{Name: "triage", Prompt: "Triage issue {{.Issue.Ref}}"}},
			Queue:   triage,
		},
		{
			Name: "development", Label: ready, MovesTo: inProgress, OnSuccess: readyToReview, OnFailure: needsAttention,
			Actions: []crew.Action{{Name: "development", Prompt: "Develop issue {{.Issue.Ref}}"}},
			Queue:   development,
		},
	}
}

// inQueues returns workflow with its stages in queues, in stage order.
func inQueues(workflow []crew.Stage, queues ...crew.Queue) []crew.Stage {
	for i, q := range queues {
		workflow[i].Queue = q
	}
	return workflow
}

// clerk is the clerk queue of 1 slot.
var clerk = crew.Queue{Name: crew.ClerkQueue, Slots: 1}

func defaultQueue(slots int) crew.Queue { return crew.Queue{Name: crew.DefaultQueue, Slots: slots} }

// takenKeys returns the keys of the issues events say were taken, in order.
func takenKeys(events []core.Event) []string {
	var keys []string
	for _, e := range events {
		if taken, ok := e.(core.IssueTaken); ok {
			keys = append(keys, taken.Issue.Key)
		}
	}
	return keys
}

// keysOf returns the keys of issues, in order.
func keysOf(issues []crew.Issue) []string {
	keys := make([]string, 0, len(issues))
	for _, i := range issues {
		keys = append(keys, i.Key)
	}
	return keys
}

// hold has d take and run held, and fails unless it holds every one.
func (d *driver) hold(held []crew.Issue) {
	d.t.Helper()
	if len(held) > 0 {
		d.running(held...)
	}
	wantHeld(d.t, d.m, keysOf(held)...)
}

// takeCases are listings and what they take, each after its held issues
// were taken.
var takeCases = []struct {
	name     string
	workflow []crew.Stage
	limit    int
	held     []crew.Issue
	listed   []crew.Issue
	want     []string
}{
	{
		// AE1: the clerk's slot is free while default is full.
		name:     "a triage issue while development fills default",
		workflow: queued(clerk, defaultQueue(2)),
		limit:    3,
		held:     []crew.Issue{issue("1", 1, ready), issue("2", 2, ready)},
		listed:   []crew.Issue{issue("3", 3, needsTriage)},
		want:     []string{"3"},
	},
	{
		// AE2: the free clerk slot is not lent to development.
		name:     "a third development issue waits although the clerk is free",
		workflow: queued(clerk, defaultQueue(2)),
		limit:    3,
		held:     []crew.Issue{issue("1", 1, ready), issue("2", 2, ready)},
		listed:   []crew.Issue{issue("3", 3, ready)},
		want:     nil,
	},
	{
		name:     "a full queue first in order does not stop the walk",
		workflow: queued(clerk, defaultQueue(2)),
		limit:    3,
		held:     []crew.Issue{issue("1", 1, ready), issue("2", 2, ready)},
		listed:   []crew.Issue{issue("3", 3, ready), issue("4", 4, needsTriage)},
		want:     []string{"4"},
	},
	{
		name:     "each queue fills to its size from one listing",
		workflow: queued(clerk, defaultQueue(2)),
		limit:    3,
		listed: []crew.Issue{
			issue("1", 1, needsTriage), issue("2", 2, needsTriage),
			issue("3", 3, ready), issue("4", 4, ready), issue("5", 5, ready),
		},
		want: []string{"3", "4", "1"},
	},
	{
		// AE4: default has 0 slots, the queue review 2.
		name:     "default with 0 slots takes nothing, another free queue does",
		workflow: queued(crew.Queue{Name: "review", Slots: 2}, defaultQueue(0)),
		limit:    3,
		listed:   []crew.Issue{issue("1", 1, ready), issue("2", 2, needsTriage)},
		want:     []string{"2"},
	},
	{
		// AE8: the two actions of #1 hold one slot.
		name:     "an issue of two actions holds one slot",
		workflow: inQueues(draft(), defaultQueue(2), clerk),
		limit:    3,
		held:     []crew.Issue{issue("1", 1, ready)},
		listed:   []crew.Issue{issue("2", 2, ready)},
		want:     []string{"2"},
	},
	{
		// AE8: #1 fills the 1-slot queue of its stage.
		name:     "an issue of two actions fills a 1-slot queue",
		workflow: inQueues(draft(), defaultQueue(1), clerk),
		limit:    3,
		held:     []crew.Issue{issue("1", 1, ready)},
		listed:   []crew.Issue{issue("2", 2, ready), issue("3", 3, readyToReview)},
		want:     []string{"3"},
	},
	{
		name:     "the higher priority first inside a queue",
		workflow: queued(defaultQueue(1), defaultQueue(1)),
		limit:    2,
		listed:   []crew.Issue{prioritized(issue("5", 1, ready), 2), prioritized(issue("6", 9, ready), 1)},
		want:     []string{"6"},
	},
	{
		name:     "the later stage first inside a queue",
		workflow: queued(defaultQueue(1), defaultQueue(1)),
		limit:    2,
		listed:   []crew.Issue{issue("5", 1, needsTriage), issue("6", 9, ready)},
		want:     []string{"6"},
	},
	{
		name:     "the oldest first inside a queue",
		workflow: queued(defaultQueue(1), defaultQueue(1)),
		limit:    2,
		listed:   []crew.Issue{issue("8", 9, ready), issue("7", 3, ready)},
		want:     []string{"7"},
	},
	{
		name:     "the global limit stops the walk though a queue has room",
		workflow: queued(clerk, defaultQueue(2)),
		limit:    2,
		held:     []crew.Issue{issue("1", 1, ready)},
		listed:   []crew.Issue{issue("2", 2, ready), issue("3", 3, needsTriage)},
		want:     []string{"2"},
	},
	{
		name:     "stages without a queue share the global limit",
		workflow: queued(crew.Queue{}, crew.Queue{}),
		limit:    2,
		held:     []crew.Issue{issue("1", 1, ready)},
		listed:   []crew.Issue{issue("2", 2, ready), issue("3", 3, needsTriage)},
		want:     []string{"2"},
	},
}

func TestTakesAnIssueOnlyWhileItsStagesQueueHasAFreeSlot(t *testing.T) {
	for _, tt := range takeCases {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, tt.workflow, tt.limit)
			d.hold(tt.held)

			_, events := d.poll(tt.listed...)
			if got := takenKeys(events); !slices.Equal(got, tt.want) {
				t.Fatalf("taken: got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnIssueWhoseVerdictMoveIsOwedKeepsItsQueuesSlot(t *testing.T) {
	d := newDriver(t, queued(clerk, defaultQueue(2)), 3)
	d.running(issue("1", 1, needsTriage))
	verdict, _ := d.send(core.SessionEnded{IssueKey: "1", Action: "triage", Outcome: succeeded})
	d.send(core.CallResult{ID: moveID(t, verdict, "1"), Result: core.ResultFailed, Reason: "timeout"})
	if c := claimOf(t, d.m, "1"); c != core.ClaimOwed {
		t.Fatalf("claim of #1: got %v, want owed", c)
	}

	cmds, _ := d.send(core.Tick{})
	wantListings(t, cmds, 1)
	_, events := d.send(core.IssuesListed{Issues: []crew.Issue{issue("2", 2, needsTriage), issue("3", 3, ready)}})
	if got := takenKeys(events); !slices.Equal(got, []string{"3"}) {
		t.Fatalf("taken: got %v, want [3]", got)
	}
}

// tickCases are the issues held when a tick comes, and whether it skips its
// listing and with which counts.
var tickCases = []struct {
	name        string
	workflow    []crew.Stage
	limit       int
	held        []crew.Issue
	skip        bool
	busy, slots int
}{
	{
		// AE9: default is full, the clerk's slot is free.
		name:     "lists while the clerk has a free slot",
		workflow: queued(clerk, defaultQueue(2)),
		limit:    3,
		held:     []crew.Issue{issue("1", 1, ready), issue("2", 2, ready)},
	},
	{
		// AE9: once a triage issue also runs.
		name:     "skips once every queue is full",
		workflow: queued(clerk, defaultQueue(2)),
		limit:    3,
		held:     []crew.Issue{issue("1", 1, ready), issue("2", 2, ready), issue("3", 3, needsTriage)},
		skip:     true, busy: 3, slots: 3,
	},
	{
		// AE9: max_parallel_issues 2 and no queue named: the clerk has
		// no stage, so its slot counts for nothing.
		name:     "counts only the queues the stages run in",
		workflow: queued(defaultQueue(1), defaultQueue(1)),
		limit:    2,
		held:     []crew.Issue{issue("1", 1, ready)},
		skip:     true, busy: 1, slots: 1,
	},
	{
		name:     "skips at the global limit though a queue has room",
		workflow: queued(clerk, defaultQueue(2)),
		limit:    2,
		held:     []crew.Issue{issue("1", 1, ready), issue("2", 2, ready)},
		skip:     true, busy: 2, slots: 2,
	},
}

func TestATickSkipsItsListingOnlyWhenNoStagesQueueHasAFreeSlot(t *testing.T) {
	for _, tt := range tickCases {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, tt.workflow, tt.limit)
			d.hold(tt.held)

			cmds, events := d.send(core.Tick{})
			if !tt.skip {
				wantListings(t, cmds, 1)
				if s := skips(events); s != nil {
					t.Fatalf("skipped polls: %#v", s)
				}
				return
			}
			wantListings(t, cmds, 0)
			wantEvents(t, skips(events), core.PollSkipped{At: d.now, Busy: tt.busy, Slots: tt.slots})
		})
	}
}

func TestAFreedQueueSlotAfterASkippedTickListsAtOnce(t *testing.T) {
	d := newDriver(t, queued(clerk, defaultQueue(2)), 3)
	d.hold([]crew.Issue{issue("1", 1, ready), issue("2", 2, ready), issue("3", 3, needsTriage)})
	cmds, _ := d.send(core.Tick{})
	wantListings(t, cmds, 0)

	verdict, _ := d.send(core.SessionEnded{IssueKey: "3", Action: "triage", Outcome: succeeded})
	cmds, _ = d.send(core.CallResult{ID: moveID(t, verdict, "3"), Result: core.ResultDone})
	wantCommands(t, cmds, core.ListIssues{States: []crew.State{needsTriage, ready}})
}

// queuesOf returns the queues m's view shows, failing unless their free
// slots are want's.
func queuesOf(t *testing.T, m *core.Model, free ...int) []core.QueueView {
	t.Helper()
	queues := m.View().Queues
	got := make([]int, 0, len(queues))
	for _, q := range queues {
		got = append(got, q.Free())
	}
	if !slices.Equal(got, free) {
		t.Errorf("free slots: got %v, want %v", got, free)
	}
	return queues
}

// queueOf returns the queue the view names for the held issue keyed key.
func queueOf(t *testing.T, m *core.Model, key string) string {
	t.Helper()
	for _, iv := range m.View().Issues {
		if iv.Issue.Key == key {
			return iv.Queue
		}
	}
	t.Fatalf("issue %s is not held", key)
	return ""
}

func wantQueues(t *testing.T, got []core.QueueView, want ...core.QueueView) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("queues: got %+v, want %+v", got, want)
	}
}

// Covers AE1 (core side).
func TestTheViewShowsEachQueuesSlotsAndBusyCountAndEachIssuesQueue(t *testing.T) {
	d := newDriver(t, queued(clerk, defaultQueue(2)), 3)
	// The later stage is taken first.
	d.hold([]crew.Issue{issue("1", 1, ready), issue("7", 7, needsTriage)})

	wantQueues(t, queuesOf(t, d.m, 0, 1),
		core.QueueView{Name: crew.ClerkQueue, Slots: 1, Busy: 1},
		core.QueueView{Name: crew.DefaultQueue, Slots: 2, Busy: 1})
	if got := queueOf(t, d.m, "7"); got != crew.ClerkQueue {
		t.Errorf("queue of #7: got %q, want clerk", got)
	}
	if got := queueOf(t, d.m, "1"); got != crew.DefaultQueue {
		t.Errorf("queue of #1: got %q, want default", got)
	}
}

// Covers AE2.
func TestAnIssueWhoseTakeIsInFlightOrOwedHoldsABusySlot(t *testing.T) {
	d := newDriver(t, queued(clerk, defaultQueue(2)), 3)
	cmds, _ := d.poll(issue("7", 7, needsTriage))
	if c := claimOf(t, d.m, "7"); c != core.ClaimTaking {
		t.Fatalf("claim of #7: got %v, want taking", c)
	}
	wantQueues(t, queuesOf(t, d.m, 0, 2),
		core.QueueView{Name: crew.ClerkQueue, Slots: 1, Busy: 1},
		core.QueueView{Name: crew.DefaultQueue, Slots: 2})

	d.send(core.CallResult{ID: moveID(t, cmds, "7"), Result: core.ResultFailed, Reason: "timeout"})
	if c := claimOf(t, d.m, "7"); c != core.ClaimOwed {
		t.Fatalf("claim of #7: got %v, want owed", c)
	}
	wantQueues(t, queuesOf(t, d.m, 0, 2),
		core.QueueView{Name: crew.ClerkQueue, Slots: 1, Busy: 1},
		core.QueueView{Name: crew.DefaultQueue, Slots: 2})
}

func TestAReleasedIssueFreesItsQueuesSlot(t *testing.T) {
	d := newDriver(t, queued(clerk, defaultQueue(2)), 3)
	d.running(issue("7", 7, needsTriage))
	verdict, _ := d.send(core.SessionEnded{IssueKey: "7", Action: "triage", Outcome: succeeded})
	d.send(core.CallResult{ID: moveID(t, verdict, "7"), Result: core.ResultDone})

	wantHeld(t, d.m)
	wantQueues(t, queuesOf(t, d.m, 1, 2),
		core.QueueView{Name: crew.ClerkQueue, Slots: 1},
		core.QueueView{Name: crew.DefaultQueue, Slots: 2})
}

// viewQueueCases are workflows and the queues the view shows for them
// before any take.
var viewQueueCases = []struct {
	name     string
	workflow []crew.Stage
	limit    int
	want     []core.QueueView
	free     []int
}{
	{
		// AE3: no stage names clerk, so it gets no line.
		name:     "only the queues some stage runs in",
		workflow: queued(defaultQueue(2), defaultQueue(2)),
		limit:    3,
		want:     []core.QueueView{{Name: crew.DefaultQueue, Slots: 2}},
		free:     []int{2},
	},
	{
		// AE4: the other queues take every slot.
		name:     "a default of 0 slots still has its line",
		workflow: queued(crew.Queue{Name: "review", Slots: 2}, defaultQueue(0)),
		limit:    2,
		want:     []core.QueueView{{Name: "review", Slots: 2}, {Name: crew.DefaultQueue}},
		free:     []int{2, 0},
	},
	{
		name:     "stages without a queue share one unnamed queue of the global limit",
		workflow: queued(crew.Queue{}, crew.Queue{}),
		limit:    2,
		want:     []core.QueueView{{Slots: 2}},
		free:     []int{2},
	},
}

func TestTheViewShowsTheQueuesTheStagesRunIn(t *testing.T) {
	for _, tt := range viewQueueCases {
		t.Run(tt.name, func(t *testing.T) {
			d := newDriver(t, tt.workflow, tt.limit)

			wantQueues(t, queuesOf(t, d.m, tt.free...), tt.want...)
		})
	}
}
