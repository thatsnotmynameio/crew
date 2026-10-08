package crew

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The question fixtures: the question blocks, whose text renders the
// issue's ref and whose return label is the test rule's ready label; the
// question action ask, which sends asked to its route ask, which posts
// blocks and moves the item to crew:question; and the comments crew's
// writers, crew-developer[bot] and you as boss, post on the item.

const (
	labelQuestion State      = "crew:question"
	blocksID      QuestionID = "blocks"
	crewWriter               = "crew-developer[bot]"
)

// writers are the logins crew posts as.
var writers = []string{crewWriter, "boss"}

// questionText returns the parsed text of a question, which must parse.
func questionText(text string) CommentTemplate {
	t, err := ParseCommentTemplate("ask", text)
	if err != nil {
		panic(err)
	}
	return t
}

// blocks is the question the test rule asks.
func blocks() Ask {
	return Ask{ID: blocksID, Text: questionText("Does {{.Issue.Ref}} block #281?"), Return: labelReady}
}

// toQuestion is the move that ends a question's route.
var toQuestion = MoveStep{To: labelQuestion}

// askedEnd is the end of a question action.
var askedEnd = EndSucceeded{Reason: NewSessionText(`crew asked the question "blocks"`)}

// askAction is the question action ask.
func askAction() Action {
	return Action{Name: "ask", Kind: QuestionSpec{Question: blocks()}, On: On{Asked: ToRoute{Route: "ask"}}}
}

// askRoute is ask's route: blocks, then the move to crew:question.
func askRoute() Route {
	return Route{Name: "ask", Steps: []Step{QuestionStep{Question: blocks()}, toQuestion}}
}

// withActions returns the test rule with the actions named in place of its
// own, among install, lfg, judge, check and ask, and ask's route.
func withActions(names ...ActionName) func(RunDefinition) RunDefinition {
	return func(d RunDefinition) RunDefinition {
		all := append(slices.Clone(d.Rule.Actions), checkAction(checkSpec()), askAction())
		d.Rule.Actions = nil
		for _, name := range names {
			i := slices.IndexFunc(all, func(a Action) bool { return a.Name == name })
			d.Rule.Actions = append(d.Rule.Actions, all[i])
		}
		d.Rule.Routes = append(slices.Clone(d.Rule.Routes), askRoute())
		return d
	}
}

// takeOf is the take at minute 0 of the actions named.
func takeOf(names ...ActionName) RunEvent {
	e, _ := taken().(RunTaken)
	e.Actions = names
	return e
}

// askEnded is ask's end at minute n with asked, and the run through ask's
// route, whose question is asked.
func askEnded(n int) []RunEvent {
	return []RunEvent{
		ActionEnded{EventHead: eh(n), Action: "ask", End: askedEnd, Verdict: Asked, Target: ToRoute{Route: "ask"}},
		RouteChosen{EventHead: eh(n), Route: "ask", Action: "ask", Steps: plans(askRoute())},
		asked(n, 0),
	}
}

// askAfterInstall are the actions install, ask, lfg and judge.
var askAfterInstall = []ActionName{"install", "ask", "lfg", "judge"}

// installingBeforeAsk is install asked in the run's new workspace, with
// ask after it.
func installingBeforeAsk() []RunEvent {
	return []RunEvent{
		takeOf(askAfterInstall...), takeMoved(), WorkspaceAsked{EventHead: eh(1)},
		WorkspaceOpened{EventHead: eh(2), Workspace: runWS(), Log: runLog},
		ActionShellAsked{EventHead: eh(2), Action: "install"},
	}
}

// badQuestion is blocks, whose text renders for the sample issue's title
// and fails on the shorter "Issue 9".
func badQuestion() QuestionStep {
	q := blocks()
	q.Text = questionText("Does {{index .Issue.Title 11}} block?")
	return QuestionStep{Question: q}
}

// badQuestionError returns the reason badQuestion's text gives.
func badQuestionError() string {
	_, err := badQuestion().Question.Text.Render(CommentData{Issue: NewIssue(testIssue())})
	if err == nil {
		panic("the bad question rendered")
	}
	return err.Error()
}

// questionDecisions decide a question action and the question and
// delegation steps of a route.
var questionDecisions = []decision{
	{
		name: "shellEnded: KTD3, a question action ends at once with asked, and the run takes the route of its name",
		def:  withActions(askAfterInstall...), given: installingBeforeAsk(),
		fact: shellEnded(3, "install", exited(0, "install passed")),
		want: seq([]RunEvent{
			ActionShellEnded{EventHead: eh(3), Action: "install", Outcome: exited(0, "install passed")},
			passedEnd(3, "install", "install passed"),
		}, askEnded(3)),
	},
	{
		name: "take: a rule whose only action is a question asks no workspace, and asks at once",
		def:  withActions("ask"), finds: true, given: []RunEvent{takeOf("ask")}, fact: landed(),
		want: seq([]RunEvent{takeMoved()}, askEnded(1)),
	},
	{
		name: "take: after a stop a question action ends without asking, and the run chooses failed",
		def:  withActions("ask"), given: seq([]RunEvent{takeOf("ask")}, stopped(0)), fact: landed(),
		want: seq([]RunEvent{takeMoved()}, stoppedAt(1, "ask")),
	},
	{
		name:  "routeChosen: R1, a route's question step is a tracker step, asked first",
		def:   failedThrough(QuestionStep{Question: blocks()}, toQuestion),
		given: inSession(), fact: lfgEnded(failedOutcome("gave up"), nil),
		want: lfgFailedThrough(QuestionStep{Question: blocks()}, toQuestion)[len(inSession()):],
	},
	{
		name:  "stepSettled: the question that landed asks the move to crew:question",
		def:   failedThrough(QuestionStep{Question: blocks()}, toQuestion),
		given: lfgFailedThrough(QuestionStep{Question: blocks()}, toQuestion),
		fact:  settled(6, 0, StepLanded{}),
		want:  []RunEvent{stepEnded(6, 0, StepLanded{}), asked(6, 1)},
	},
	{
		name:  "stop: a question in flight goes on, as every tracker step does",
		def:   failedThrough(QuestionStep{Question: blocks()}, toQuestion),
		given: lfgFailedThrough(QuestionStep{Question: blocks()}, toQuestion), fact: StopReached{FactHead: fh(6)},
		want: stopped(6),
	},
	{
		name: "routeChosen: a question whose text does not render fails unasked, and the route goes on",
		def:  failedThrough(badQuestion(), toQuestion), given: inSession(), fact: lfgEnded(failedOutcome("gave up"), nil),
		want: []RunEvent{
			lfgSession(failedOutcome("gave up")),
			lfgEnd(failedBy(NewSessionText("gave up"), CauseSession), toFailed),
			failedRoute(5, badQuestion(), toQuestion),
			stepEnded(5, 0, StepFailed{Reason: NewShellReason(badQuestionError())}),
			asked(5, 1),
		},
	},
	{
		name: "stepSettled: KTD8, the delegation that landed asks the move",
		def:  failedThrough(DelegateStep{}, moveFailed), given: lfgFailedThrough(DelegateStep{}, moveFailed),
		fact: settled(6, 0, StepLanded{}),
		want: []RunEvent{stepEnded(6, 0, StepLanded{}), asked(6, 1)},
	},
}

func TestDecideTheQuestion(t *testing.T) { decide(t, questionDecisions) }

func TestTheStepPlansOfAQuestionAndADelegation(t *testing.T) {
	got := plans(Route{Steps: []Step{QuestionStep{Question: blocks()}, DelegateStep{}, toQuestion}})
	want := []StepPlan{{Kind: StepQuestion, Question: blocksID}, {Kind: StepDelegate}, {Kind: StepMove, To: labelQuestion}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("plans = %#v, want %#v", got, want)
	}
	if !StepQuestion.Delivered() || !StepDelegate.Delivered() {
		t.Errorf("a question or a delegation is not a tracker step")
	}
}

func TestAQuestionsBodyIsItsTextThenItsMarker(t *testing.T) {
	d := CommentData{Issue: sampleIssue(), Rule: "deps"}
	body, err := blocks().Body(d)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if lines[0] != "Does #42 block #281?" || lines[len(lines)-1] != QuestionMarker(blocksID, "deps", labelReady) {
		t.Errorf("Body = %q, want the rendered text, then the marker of blocks asked by deps", body)
	}
}

func TestARuleWhoseOnlyActionIsAQuestionNeedsNoWorkspace(t *testing.T) {
	if withActions("ask")(sequence()).Rule.needsWorkspace() {
		t.Errorf("a rule of ask alone needs a workspace, want none")
	}
	if !withActions("ask", "lfg")(sequence()).Rule.needsWorkspace() {
		t.Errorf("a rule of ask and lfg needs no workspace, want lfg's")
	}
}

// writes is a comment login posted through crew's tracker, which adds
// crew's own marker.
func writes(login, body string) Comment {
	return Comment{Author: login, App: strings.HasSuffix(login, "[bot]"), Body: body + "\n\n" + PostedMarker + "\n"}
}

// asksAs is the comment login posted to ask the question id of the rule
// deps.
func asksAs(login string, id QuestionID) Comment {
	return writes(login, "Does #9 block #281?\n\n"+QuestionMarker(id, "deps", labelReady))
}

// delegates is the comment login posted to delegate the question id.
func delegates(login string, id QuestionID) Comment {
	return writes(login, "@octocat, please answer.\n\n"+DelegatedMarker(id))
}

// posted is the question the deps rule posted.
func posted(id QuestionID) PostedQuestion {
	return PostedQuestion{ID: id, Rule: "deps", Return: labelReady}
}

// openQuestionCases are comment lists OpenQuestion reads with writers, and
// the open question it finds, when found is set.
var openQuestionCases = []struct {
	name     string
	comments []Comment
	want     PostedQuestion
	found    bool
}{
	{
		name: "R14: of two questions by crew's writer the later one is open",
		comments: []Comment{
			asksAs(crewWriter, "first"), person("alice", "Looking"), asksAs(crewWriter, "second"),
		},
		want: posted("second"), found: true,
	},
	{
		name:     "a question followed by its delegation is not open, and there is no other",
		comments: []Comment{asksAs(crewWriter, blocksID), delegates(crewWriter, blocksID)},
	},
	{
		name: "a question followed by the delegation that could not read is not open",
		comments: []Comment{
			asksAs(crewWriter, blocksID), writes(crewWriter, "@octocat, please answer.\n\n"+UnreadDelegatedMarker),
		},
	},
	{
		name: "an earlier question delegated, then a later one whose post the tracker refused: none",
		comments: []Comment{
			asksAs(crewWriter, "earlier"), delegates(crewWriter, "earlier"), person("alice", "Yes"),
		},
	},
	{
		name: "a question after an earlier one's delegation is open",
		comments: []Comment{
			asksAs(crewWriter, "earlier"), delegates(crewWriter, "earlier"), asksAs(crewWriter, blocksID),
		},
		want: posted(blocksID), found: true,
	},
	{
		name: "a question marker without crew's own marker is no question",
		comments: []Comment{
			{Author: crewWriter, App: true, Body: "Does #9 block?\n\n" + QuestionMarker(blocksID, "deps", labelReady)},
		},
	},
	{
		name:     "a question by a login other than the writers is no question",
		comments: []Comment{asksAs("mallory", blocksID)},
	},
	{
		name:     "a delegation by a login other than the writers closes nothing",
		comments: []Comment{asksAs(crewWriter, blocksID), delegates("mallory", blocksID)},
		want:     posted(blocksID), found: true,
	},
	{
		name:     "a writer's login in another letter case is the writer's",
		comments: []Comment{asksAs("Crew-Developer[bot]", blocksID)},
		want:     posted(blocksID), found: true,
	},
	{
		name:     "your own question, as the writer boss, is one",
		comments: []Comment{asksAs("boss", blocksID)},
		want:     posted(blocksID), found: true,
	},
	{
		name:     "a writer's comment with crew's marker and no question marker is no question",
		comments: []Comment{writes(crewWriter, "crew's report")},
	},
	{
		name: "no comments: none",
	},
	{
		name:     "only comments that do not qualify: none",
		comments: []Comment{person("alice", "Does #9 block #281?"), writes("mallory", "Hi")},
	},
}

func TestOpenQuestionFindsTheLatestQuestionCrewPostedAndDidNotDelegate(t *testing.T) {
	for _, tc := range openQuestionCases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := OpenQuestion(tc.comments, writers)
			if found != tc.found || got != tc.want {
				t.Errorf("OpenQuestion = %#v, %v, want %#v, %v", got, found, tc.want, tc.found)
			}
		})
	}
}

// askedAt are the facts of a run of the actions named whose question ask,
// after them, ended with asked, and whose route landed. A run with install
// opens its workspace and runs it first.
func askedAt(names ...ActionName) []Fact {
	facts := []Fact{landed()}
	if slices.Contains(names, "install") {
		facts = append(facts, ready(false), shellEnded(3, "install", exited(0, "install passed")))
	}
	return append(facts, settled(7, 0, StepLanded{}), settled(8, 1, StepLanded{}))
}

// askedPast is a run of the actions named whose question ask ended with
// asked.
func askedPast(names ...ActionName) past {
	return past{take: takeOf(names...), change: withActions(names...), facts: askedAt(names...)}
}

// askedAtLfg is the start at lfg, the action after ask, in the run's
// worktree.
var askedAtLfg = StartAt{
	Workspace: runWS(), Log: runLog, Action: "lfg", Route: "ask", Reason: askedEnd.Reason,
}

// questionStarts are the starts after a run whose question ask ended with
// asked (KTD4), and after a run whose session chose a route that asks.
func questionStarts() []startCase {
	return []startCase{
		{
			name: "asked, followed by a session: at the session, in the run's worktree",
			past: askedPast(askAfterInstall...), rule: withActions(askAfterInstall...), want: askedAtLfg,
		},
		{
			name: "asked, the rule's last action: the passed route alone, in the run's worktree",
			past: askedPast("install", "ask"), rule: withActions("install", "ask"),
			want: StartPassedRoute{Workspace: Some(runWS()), Log: runLog},
		},
		{
			name: "asked, the rule's only action: the passed route alone, without a worktree",
			past: askedPast("ask"), rule: withActions("ask"), want: StartPassedRoute{},
		},
		{
			name: "asked without a worktree, followed by a function: fresh, asking again",
			past: askedPast("ask", "check"), rule: withActions("ask", "check"), want: StartFresh{},
		},
		{
			name: "a session's verdict chose a route that asks: at that session, as any route",
			past: past{change: asksWhenBlocked, facts: slices.Concat(toJudge()[:4], []Fact{
				lfgEnded(succeeded("done"), VerdictReported{Verdict: "blocked"}),
				settled(6, 0, StepLanded{}), settled(7, 1, StepLanded{}),
			})},
			rule: asksWhenBlocked, want: atAction("lfg", "blocked", "done"),
		},
	}
}

func TestTheStartOfARunAfterAQuestion(t *testing.T) {
	for _, tc := range questionStarts() {
		t.Run(tc.name, func(t *testing.T) {
			h := folded(tc.past.events(t))
			if got := h.Start(testID, tc.rule(sequence()).Rule); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Start =\n%#v\nwant\n%#v", got, tc.want)
			}
		})
	}
}

// Another run that opens the asking run's worktree retires it: an action
// after the question starts fresh, asking again, and the passed route alone
// runs without the worktree.
func TestARetiredWorktreeAfterAQuestion(t *testing.T) {
	for _, tc := range []struct {
		names []ActionName
		want  Start
	}{
		{names: askAfterInstall, want: StartFresh{}},
		{names: []ActionName{"install", "ask"}, want: StartPassedRoute{}},
	} {
		h := folded(askedPast(tc.names...).events(t))
		h.Retire(runWS().Name, IssueID{Repository: "R_1", Key: "10"}, "implement")
		if got := h.Start(testID, withActions(tc.names...)(sequence()).Rule); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Start after %v = %#v, want %#v", tc.names, got, tc.want)
		}
	}
}
