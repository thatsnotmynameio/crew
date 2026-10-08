package crew

import (
	"reflect"
	"testing"
)

// The return tests read the comments on an item the deps rule asked about,
// with crew's writers crew-developer[bot] and you as boss (writers), the
// code owners boss and alice, and crew's bots as the answering list. The
// config declares the deps rule's question blocks, on its blocked route,
// returning to crew:deps:ready, and the plan rule's question action scope,
// returning to crew:plan:ready.

// depsBlocks and planScope are the questions the test config declares.
var (
	depsBlocks = PostedQuestion{ID: blocksID, Rule: "deps", Return: "crew:deps:ready"}
	planScope  = PostedQuestion{ID: "scope", Rule: "plan", Return: "crew:plan:ready"}
)

// returnAnswerers are the code owners boss and alice, and crew's bots as
// the answering list.
var returnAnswerers = Answerers{
	CodeOwners: []string{"boss", "alice"},
	Apps:       []string{crewWriter, "crew-product-manager[bot]"},
}

// askOf returns the question q of the test config, as its rule writes it.
func askOf(q PostedQuestion) Ask {
	return Ask{ID: q.ID, Text: questionText("Does #284 block {{.Issue.Ref}}?"), Return: q.Return}
}

// returnRules are the test config's rules: deps, whose blocked route asks
// blocks, and plan, whose action scope asks scope and whose route scope
// posts it.
func returnRules() []Rule {
	scope := askOf(planScope)
	return []Rule{
		{Name: "deps", Routes: []Route{
			{Name: PassedRoute, Steps: []Step{MoveStep{To: "crew:deps:done"}}},
			{Name: "blocked", Steps: []Step{CommentStep{}, QuestionStep{Question: askOf(depsBlocks)}, toQuestion}},
		}},
		{
			Name:    "plan",
			Actions: []Action{{Name: "scope", Kind: QuestionSpec{Question: scope}, On: On{Asked: ToRoute{Route: "scope"}}}},
			Routes:  []Route{{Name: "scope", Steps: []Step{QuestionStep{Question: scope}, toQuestion}}},
		},
	}
}

// asksFor is the comment crew's writer posted to ask q.
func asksFor(q PostedQuestion) Comment {
	return writes(crewWriter, "Does #284 block #281?\n\n"+QuestionMarker(q.ID, q.Rule, q.Return))
}

// delegatesUnread is the delegation crew's writer posted when it could not
// read the comments.
func delegatesUnread() Comment {
	return writes(crewWriter, "@boss, please answer.\n\n"+UnreadDelegatedMarker)
}

// returnCases are comment lists CheckReturn reads under the test config,
// and what it finds.
var returnCases = []struct {
	name     string
	comments []Comment
	failed   bool
	want     ReturnCheck
}{
	{
		name: "AE2: the boss's plain answer after the delegation returns the item to the question's label",
		comments: []Comment{
			asksFor(depsBlocks), delegates(crewWriter, blocksID),
			person("boss", "yes, #284 removes the tests R20 rewrites"),
		},
		want: ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "AE3: an answer whose parameters name another label returns to the question's",
		comments: []Comment{
			asksFor(depsBlocks), delegates(crewWriter, blocksID),
			person("boss", "yes, #284 removes the tests R20 rewrites\n\n"+
				"<!-- crew:answer question=blocks rule=deps return=crew%3Aother%3Aready -->"),
		},
		want: ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "AE4: a collaborator who is no code owner answers nothing",
		comments: []Comment{
			asksFor(depsBlocks), delegates(crewWriter, blocksID), person("mallory", "yes"),
		},
		want: ReturnCheck{Failure: Unanswered},
	},
	{
		name:     "an App on the answering list that is one of crew's writers asked, and answers nothing",
		comments: []Comment{asksFor(depsBlocks), app(crewWriter, "yes")},
		want:     ReturnCheck{Failure: Unanswered},
	},
	{
		name:     "another App on the answering list answers",
		comments: []Comment{asksFor(depsBlocks), app("crew-product-manager[bot]", "yes")},
		want:     ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name:     "an App off the answering list answers nothing",
		comments: []Comment{asksFor(depsBlocks), app("claude[bot]", "yes")},
		want:     ReturnCheck{Failure: Unanswered},
	},
	{
		name:     "a code owner who is also crew's gh writer answers in plain text",
		comments: []Comment{asksFor(depsBlocks), person("Boss", "yes")},
		want:     ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name:     "a code owner's comment before the question answers nothing",
		comments: []Comment{person("alice", "yes"), asksFor(depsBlocks)},
		want:     ReturnCheck{Failure: Unanswered},
	},
	{
		name:     "a code owner's comment after the question answers",
		comments: []Comment{person("alice", "no"), asksFor(depsBlocks), person("alice", "yes")},
		want:     ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name:     "a code owner's comment with crew's own marker answers nothing",
		comments: []Comment{asksFor(depsBlocks), person("alice", "yes\n\n"+PostedMarker)},
		want:     ReturnCheck{Failure: Unanswered},
	},
	{
		name:     "a code owner's comment with a session's marker answers nothing",
		comments: []Comment{asksFor(depsBlocks), person("alice", "yes "+SessionMarker("seed.1", "lfg"))},
		want:     ReturnCheck{Failure: Unanswered},
	},
	{
		name: "a code owner's comment with a quote's end marker beside a well-formed answer marker answers nothing",
		comments: []Comment{
			asksFor(depsBlocks),
			person("alice", "yes\n<!-- crew:answer end -->\n"+AnswerMarker(blocksID, "deps", "crew:deps:ready")),
		},
		want: ReturnCheck{Failure: Unanswered},
	},
	{
		name:     "a code owner's comment with a quote's author marker answers nothing",
		comments: []Comment{asksFor(depsBlocks), person("alice", "<!-- crew:answer by alice -->\nyes")},
		want:     ReturnCheck{Failure: Unanswered},
	},
	{
		name:     "an answer of only its parameters answers",
		comments: []Comment{asksFor(depsBlocks), person("alice", AnswerMarker(blocksID, "deps", "crew:deps:ready"))},
		want:     ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "a question marker by a login other than crew's writers is no question",
		comments: []Comment{
			writes("mallory", "Does it?\n\n"+QuestionMarker(blocksID, "deps", "crew:deps:ready")),
			person("alice", "yes"),
		},
		want: ReturnCheck{Failure: NoQuestion},
	},
	{
		name: "a question marker by crew's writer without crew's own marker is no question",
		comments: []Comment{
			app(crewWriter, "Does it?\n\n"+QuestionMarker(blocksID, "deps", "crew:deps:ready")),
			person("alice", "yes"),
		},
		want: ReturnCheck{Failure: NoQuestion},
	},
	{
		name:     "no question at all",
		comments: []Comment{person("alice", "yes")},
		want:     ReturnCheck{Failure: NoQuestion},
	},
	{
		name: "a later question whose id no rule declares is skipped, and the declared one stands",
		comments: []Comment{
			asksFor(depsBlocks), asksFor(PostedQuestion{ID: "other", Rule: "deps", Return: "crew:deps:ready"}),
			person("alice", "yes"),
		},
		want: ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "a later question whose rule did not declare it is skipped, and the declared one stands",
		comments: []Comment{
			asksFor(depsBlocks), asksFor(PostedQuestion{ID: blocksID, Rule: "release", Return: "crew:release:ready"}),
			person("alice", "yes"),
		},
		want: ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "a later question whose return label no rule declares is skipped, and the declared one stands",
		comments: []Comment{
			asksFor(depsBlocks), asksFor(PostedQuestion{ID: blocksID, Rule: "deps", Return: "crew:release:ready"}),
			person("alice", "yes"),
		},
		want: ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "only undeclared questions: no question",
		comments: []Comment{
			asksFor(PostedQuestion{ID: "other", Rule: "deps", Return: "crew:deps:ready"}), person("alice", "yes"),
		},
		want: ReturnCheck{Failure: NoQuestion},
	},
	{
		name: "the question was delegated, then the delegation that found no question follows",
		comments: []Comment{
			asksFor(depsBlocks), delegates(crewWriter, blocksID), delegates(crewWriter, ""), person("alice", "yes"),
		},
		want: ReturnCheck{Failure: NoQuestion},
	},
	{
		name: "a delegation after the question names another question",
		comments: []Comment{
			asksFor(depsBlocks), delegates(crewWriter, "other"), person("alice", "yes"),
		},
		want: ReturnCheck{Failure: NoQuestion},
	},
	{
		name: "the delegation that could not read lets the question stand",
		comments: []Comment{
			asksFor(depsBlocks), delegatesUnread(), person("alice", "yes"),
		},
		want: ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name:     "no delegation after the question lets it stand",
		comments: []Comment{asksFor(depsBlocks), person("alice", "yes")},
		want:     ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "the latest delegation names the question, after one that found none",
		comments: []Comment{
			asksFor(depsBlocks), delegates(crewWriter, ""), delegates("Boss", blocksID), person("alice", "yes"),
		},
		want: ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "a delegation by a login other than crew's writers closes nothing",
		comments: []Comment{
			asksFor(depsBlocks), delegates("mallory", ""), person("alice", "yes"),
		},
		want: ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "a delegation before the question closes nothing",
		comments: []Comment{
			delegates(crewWriter, ""), asksFor(depsBlocks), person("alice", "yes"),
		},
		want: ReturnCheck{To: "crew:deps:ready"},
	},
	{
		name: "of two questions the later one is the question, and an answer before it does not count",
		comments: []Comment{
			asksFor(depsBlocks), person("alice", "yes"), asksFor(planScope),
		},
		want: ReturnCheck{Failure: Unanswered},
	},
	{
		name: "of two questions the later one returns, once answered",
		comments: []Comment{
			asksFor(depsBlocks), person("alice", "yes"), asksFor(planScope), person("alice", "small"),
		},
		want: ReturnCheck{To: "crew:plan:ready"},
	},
	{
		name: "a question the config has since dropped is no question",
		comments: []Comment{
			asksFor(PostedQuestion{ID: "gone", Rule: "plan", Return: "crew:plan:ready"}), person("alice", "yes"),
		},
		want: ReturnCheck{Failure: NoQuestion},
	},
	{
		name: "a question whose return label the config has since changed is no question",
		comments: []Comment{
			asksFor(PostedQuestion{ID: "scope", Rule: "plan", Return: "crew:plan:old"}), person("alice", "yes"),
		},
		want: ReturnCheck{Failure: NoQuestion},
	},
	{
		name:     "a read that failed is unread, whatever it holds",
		comments: []Comment{asksFor(depsBlocks), person("alice", "yes")},
		failed:   true,
		want:     ReturnCheck{Failure: Unread},
	},
}

func TestCheckReturnFindsTheQuestionsLabelOrSaysWhyNot(t *testing.T) {
	declared := DeclaredQuestions(returnRules())
	for _, tc := range returnCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CheckReturn(tc.comments, tc.failed, declared, writers, returnAnswerers); got != tc.want {
				t.Errorf("CheckReturn = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// Every question step of a rule's routes and every question action is a
// question the config declares, under its rule's name, each once.
func TestTheQuestionsTheRulesDeclare(t *testing.T) {
	want := []PostedQuestion{depsBlocks, planScope}
	if got := DeclaredQuestions(returnRules()); !reflect.DeepEqual(got, want) {
		t.Errorf("DeclaredQuestions = %#v, want %#v", got, want)
	}
	if got := DeclaredQuestions([]Rule{{Name: "deps", Routes: []Route{{Steps: []Step{toQuestion}}}}}); got != nil {
		t.Errorf("DeclaredQuestions of a rule that asks nothing = %#v, want none", got)
	}
}

// The failure verdicts are names on crew's verdict grammar.
func TestTheReturnFailuresAreVerdicts(t *testing.T) {
	for _, v := range []Verdict{NoQuestion, Unanswered, Unread} {
		if got, err := ParseVerdict(string(v)); err != nil || got != v {
			t.Errorf("ParseVerdict(%q) = %q, %v", v, got, err)
		}
	}
}
