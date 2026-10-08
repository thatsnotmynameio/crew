package engine_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"testing/synctest"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/engine"
	"github.com/thatsnotmynameio/crew/internal/fake"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// The labels of crew's question rule.
const (
	questionLabel   crew.State = "crew:question"
	questionWaiting crew.State = "crew:question:waiting answer"
)

// questioning is crew's question rule, a rule without actions: its passed
// route delegates the issue's open question, then moves the issue to
// crew:question:waiting answer.
var questioning = crew.Rule{
	Name:   "question",
	Labels: crew.Labels{Ready: questionLabel, Running: "crew:question:in progress"},
	Routes: []crew.Route{{Name: crew.PassedRoute, Steps: []crew.Step{
		crew.DelegateStep{}, crew.MoveStep{To: questionWaiting},
	}}},
}

// postedQuestion is the comment that asks deps's question blocks on #1,
// posted as author, with crew's own marker as the tracker writes it.
func postedQuestion(author string) crew.Comment {
	return crew.Comment{
		Author: author,
		Body: "Does #1 block #281?\n\n" + crew.QuestionMarker("blocks", "deps", "crew:deps:ready") + "\n" +
			crew.PostedMarker,
	}
}

// delegationOf is the delegation of #1's question to octocat, as search
// found it: deps's question blocks when found.
func delegationOf(search crew.QuestionSearch) crew.Delegation {
	d := crew.Delegation{IssueID: issueID("1"), IssueRef: "#1", Answerer: "octocat", Search: search}
	if search == crew.QuestionFound {
		d.ID, d.Rule = "blocks", "deps"
	}
	return d
}

// delegated runs cfg, whose answerer is octocat, until #1 left the question
// rule, stops it, and fails unless the tracker rt got want alone and #1
// waits for an answer.
func delegated(t *testing.T, cfg engine.Config, rt fake.RoutingTracker, want crew.Delegation) {
	t.Helper()
	cfg.Answerer = "octocat"
	r := start(t, cfg)
	synctest.Wait()
	stopped(t, r)

	if got := rt.Delegations(); !reflect.DeepEqual(got, []crew.Delegation{want}) {
		t.Errorf("delegations = %#v, want %#v", got, want)
	}
	if got := states(t, rt, "1"); !slices.Equal(got, []crew.State{questionWaiting}) {
		t.Errorf("#1 is in %v, want %s", got, questionWaiting)
	}
}

// Covers AE1, R4, R5, KTD8, KTD11: the engine reads the comments and
// delegates the question crew posted, as your gh login or as the default
// bot that acts, to the answerer it was given.
func TestTheQuestionRuleDelegatesTheQuestionCrewPostedToTheAnswerer(t *testing.T) {
	for _, author := range []string{"boss", "crew-clerk[bot]"} {
		t.Run(author, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rt := fake.NewRoutingTracker(issue(1, questionLabel))
				rt.SetComments("1", postedQuestion(author))
				cfg := config(t, listing{rt}, questioning)
				cfg.DefaultBot, cfg.Bots = "clerk", []crew.BotName{"clerk"}
				cfg.Identities = map[crew.BotName]port.Identity{"clerk": {Login: "crew-clerk[bot]"}}

				delegated(t, cfg, rt, delegationOf(crew.QuestionFound))
			})
		})
	}
}

// Covers KTD8: a listing that fails still delegates, saying crew could not
// read the comments, and the issue still moves on.
func TestAFailedReadStillDelegates(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt := fake.NewRoutingTracker(issue(1, questionLabel))
		rt.FailCommentLists("1", errors.New("gh: HTTP 502"))

		delegated(t, config(t, listing{rt}, questioning), rt, delegationOf(crew.QuestionUnread))
	})
}

// Covers KTD9: a tracker that cannot delegate refuses the delegation, which
// is given up, and the route's move still runs.
func TestATrackerThatCannotDelegateGivesTheDelegationUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tr := fake.NewTracker(issue(1, questionLabel))
		cfg := config(t, tr, questioning)
		cfg.Answerer = "octocat"
		r := start(t, cfg)
		synctest.Wait()
		stopped(t, r)

		want := []crew.StepOutcome{
			crew.StepGivenUp{Reason: "the tracker cannot delegate: the tracker refused"}, crew.StepLanded{},
		}
		if got := r.stepOutcomes(); !reflect.DeepEqual(got, want) {
			t.Errorf("step outcomes = %#v, want %#v", got, want)
		}
		if got := states(t, tr, "1"); !slices.Equal(got, []crew.State{questionWaiting}) {
			t.Errorf("#1 is in %v, want %s", got, questionWaiting)
		}
	})
}

// hanging is listing over a tracker whose comment listings never return
// before their context ends.
type hanging struct {
	listing
}

// Comments implements port.CommentLister.
func (hanging) Comments(ctx context.Context, _ crew.IssueID) ([]crew.Comment, error) {
	<-ctx.Done()
	return nil, fmt.Errorf("list the comments: %w", ctx.Err())
}

// Covers KTD8: the read posts the comments the tracker listed, or a failed
// read with its scrubbed reason when the listing fails, times out or the
// tracker lists no comments.
func TestTheReadOfAQuestionPostsTheCommentsOrWhyItFailed(t *testing.T) {
	rt := fake.NewRoutingTracker(issue(1, questionLabel))
	rt.SetComments("1", postedQuestion("boss"))
	tests := []struct {
		name    string
		tracker func(root string) port.Tracker
		want    core.QuestionRead
	}{
		{
			name:    "listed",
			tracker: func(string) port.Tracker { return listing{rt} },
			want:    core.QuestionRead{Comments: []crew.Comment{postedQuestion("boss")}},
		},
		{
			name: "failed",
			tracker: func(root string) port.Tracker {
				rt.FailCommentLists("1", fmt.Errorf("open %s/.git: denied", root))
				return listing{rt}
			},
			want: core.QuestionRead{Failed: true},
		},
		{
			name:    "timed out",
			tracker: func(string) port.Tracker { return hanging{listing{rt}} },
			want:    core.QuestionRead{Failed: true},
		},
		{
			name:    "no lister",
			tracker: func(string) port.Tracker { return fake.NewTracker(issue(1, questionLabel)) },
			want:    core.QuestionRead{Failed: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := config(t, nil, questioning)
				cfg.Tracker = tt.tracker(cfg.Root)
				c := core.ReadQuestion{IssueID: issueID("1"), Run: "run", Step: 0}

				got := engine.New(cfg).ReadQuestion(context.Background(), c)
				want := tt.want
				want.IssueID, want.Run = c.IssueID, c.Run
				if !reflect.DeepEqual(got, want) {
					t.Errorf("read = %#v, want %#v", got, want)
				}
			})
		})
	}
}

// The labels of crew's answered rule, and the ready label of deps.
const (
	answeredLabel crew.State = "crew:answered"
	answeredFail  crew.State = "crew:answered:failed"
	depsReady     crew.State = "crew:deps:ready"
)

// answeredRule is crew's answered rule: its one action, answer, checks the
// answer; passed returns the item to the label the check found, and failed
// reports, then moves the item to crew:answered:failed.
var answeredRule = crew.Rule{
	Name:    "answered",
	Labels:  crew.Labels{Ready: answeredLabel, Running: "crew:answered:in progress"},
	Actions: []crew.Action{{Name: "answer", Kind: crew.ReturnSpec{}}},
	Routes: []crew.Route{
		{Name: crew.PassedRoute, Steps: []crew.Step{crew.ReturnStep{}}},
		{Name: crew.FailedRoute, Steps: []crew.Step{crew.ReportStep{}, crew.MoveStep{To: answeredFail}}},
	},
}

// depsRule is the rule deps, whose session check, on unsure, asks the
// question blocks, to return to deps's ready label, and moves the issue to
// crew:question.
func depsRule(t *testing.T) crew.Rule {
	t.Helper()
	text, err := crew.ParseCommentTemplate("ask", "Does {{.Issue.Ref}} block #281?")
	if err != nil {
		t.Fatal(err)
	}
	check := sessionAction("check", "Check {{.Issue.Ref}}")
	check.On = crew.On{"unsure": crew.ToRoute{Route: "ask"}}
	return crew.Rule{
		Name: "deps", Labels: crew.Labels{Ready: depsReady, Running: "crew:deps:in progress"},
		Actions: []crew.Action{check},
		Routes: append(routes(needsAttention), crew.Route{Name: "ask", Steps: []crew.Step{
			crew.QuestionStep{Question: crew.Ask{ID: "blocks", Text: text, Return: depsReady}},
			crew.MoveStep{To: questionLabel},
		}}),
	}
}

// Covers AE2, AE4, R9, R10, KTD2: the engine reads #1's comments for the
// answered rule's check, which returns #1 to deps's ready label once alice
// answered the question crew posted as you, and fails it to
// crew:answered:failed, with a report, when it could not read them.
func TestTheAnsweredRuleReturnsTheItemOnceTheEngineReadTheAnswer(t *testing.T) {
	tests := []struct {
		name   string
		listed func(rt fake.RoutingTracker)
		want   crew.State
		report bool
	}{
		{
			name: "answered",
			listed: func(rt fake.RoutingTracker) {
				rt.SetComments("1", postedQuestion("boss"), crew.Comment{Author: "alice", Body: "yes"})
			},
			want: depsReady,
		},
		{
			name:   "read failed",
			listed: func(rt fake.RoutingTracker) { rt.FailCommentLists("1", errors.New("gh: HTTP 502")) },
			want:   answeredFail, report: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rt := fake.NewRoutingTracker(issue(1, answeredLabel))
				tt.listed(rt)
				r := start(t, config(t, listing{rt}, answeredRule, depsRule(t)))
				synctest.Wait()
				stopped(t, r)

				if got := states(t, rt, "1"); !slices.Equal(got, []crew.State{tt.want}) {
					t.Errorf("#1 is in %v, want %s", got, tt.want)
				}
				if got := len(rt.Reports()); got > 0 != tt.report {
					t.Errorf("%d reports posted, want a report: %v", got, tt.report)
				}
			})
		})
	}
}

// Covers KTD2: the read of the answered rule's check posts the comments the
// tracker listed, or a failed read when the listing fails, times out or
// the tracker lists no comments.
func TestTheReadOfAReturnPostsTheCommentsOrThatItFailed(t *testing.T) {
	rt := fake.NewRoutingTracker(issue(1, answeredLabel))
	rt.SetComments("1", postedQuestion("boss"))
	tests := []struct {
		name    string
		tracker func(root string) port.Tracker
		want    core.ReturnRead
	}{
		{
			name:    "listed",
			tracker: func(string) port.Tracker { return listing{rt} },
			want:    core.ReturnRead{Comments: []crew.Comment{postedQuestion("boss")}},
		},
		{
			name: "failed",
			tracker: func(root string) port.Tracker {
				rt.FailCommentLists("1", fmt.Errorf("open %s/.git: denied", root))
				return listing{rt}
			},
			want: core.ReturnRead{Failed: true},
		},
		{
			name:    "timed out",
			tracker: func(string) port.Tracker { return hanging{listing{rt}} },
			want:    core.ReturnRead{Failed: true},
		},
		{
			name:    "no lister",
			tracker: func(string) port.Tracker { return fake.NewTracker(issue(1, answeredLabel)) },
			want:    core.ReturnRead{Failed: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := config(t, nil, answeredRule)
				cfg.Tracker = tt.tracker(cfg.Root)
				c := core.ReadReturn{IssueID: issueID("1"), Run: "run", Action: "answer"}

				got := engine.New(cfg).ReadReturn(context.Background(), c)
				want := tt.want
				want.IssueID, want.Run, want.Action = c.IssueID, c.Run, c.Action
				if !reflect.DeepEqual(got, want) {
					t.Errorf("read = %#v, want %#v", got, want)
				}
			})
		})
	}
}
