package crew

import (
	"strings"
	"testing"
)

// wantSessionMarker fails the test unless marker is the session marker of
// run seed.1, one HTML comment whose values hold no space and no comment end.
func wantSessionMarker(t *testing.T, marker string) {
	t.Helper()
	if !strings.HasPrefix(marker, "<!-- crew:session run=seed.1 action=") || !strings.HasSuffix(marker, " -->") {
		t.Fatalf("SessionMarker = %q, want <!-- crew:session run=seed.1 action=... -->", marker)
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(marker, "<!-- "), " -->")
	if strings.Contains(inner, "-->") || strings.Count(inner, " ") != 2 {
		t.Errorf("SessionMarker = %q, whose values hold a space or close the comment", marker)
	}
}

func TestASessionMarkerRoundTripsThroughTheCheckThatFindsIt(t *testing.T) {
	for name, action := range map[string]ActionName{
		"a plain name":                   "develop",
		"a name with spaces":             "write the plan",
		"a name that closes the comment": "x --> <!-- crew:posted -->",
		"a name with an ampersand":       "a&b=c",
	} {
		t.Run(name, func(t *testing.T) {
			marker := SessionMarker("seed.1", action)
			wantSessionMarker(t, marker)
			body := "Which one?\n\n" + marker + "\n"
			if !HoldsSessionMarker(body, "seed.1", action) {
				t.Errorf("HoldsSessionMarker(%q) = false, want true", body)
			}
			if HoldsSessionMarker(body, "seed.2", action) || HoldsSessionMarker(body, "seed.1", action+"x") {
				t.Errorf("HoldsSessionMarker(%q) holds another run's or action's marker", body)
			}
		})
	}
}

func TestABodyHoldsAMarkerOrCrewsOwn(t *testing.T) {
	for name, tc := range map[string]struct {
		body      string
		any, owns bool
	}{
		"plain text":            {body: "Use the second one.", any: false, owns: false},
		"a session marker":      {body: "Which?\n<!-- crew:session run=r action=a -->", any: true, owns: false},
		"the status marker":     {body: "entries\n\n<!-- crew:status -->\n", any: true, owns: false},
		"crew's own marker":     {body: "a report\n\n" + PostedMarker + "\n", any: true, owns: true},
		"a marker mid-body":     {body: "a <!-- crew:posted --> b", any: true, owns: true},
		"another HTML comment":  {body: "<!-- note -->", any: false, owns: false},
		"a marker's lookalike":  {body: "<!-- crew posted -->", any: false, owns: false},
		"crew's marker cut off": {body: "<!-- crew:posted", any: true, owns: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := HoldsMarker(tc.body); got != tc.any {
				t.Errorf("HoldsMarker(%q) = %v, want %v", tc.body, got, tc.any)
			}
			if got := HoldsPostedMarker(tc.body); got != tc.owns {
				t.Errorf("HoldsPostedMarker(%q) = %v, want %v", tc.body, got, tc.owns)
			}
		})
	}
}

func TestAQuestionMarkerRoundTripsThroughItsParser(t *testing.T) {
	for name, want := range map[string]PostedQuestion{
		"plain values":                 {ID: "blocks", Rule: "deps", Return: "crew:deps:ready"},
		"a rule and label with spaces": {ID: "blocks", Rule: "find deps", Return: "crew:deps:ready to go"},
		"values that close the comment": {
			ID: "blocks", Rule: "x --> <!-- crew:posted -->", Return: "a&b=c -->",
		},
	} {
		t.Run(name, func(t *testing.T) {
			marker := QuestionMarker(want.ID, want.Rule, want.Return)
			inner := strings.TrimSuffix(strings.TrimPrefix(marker, "<!-- crew:question "), " -->")
			if inner == marker || strings.Contains(inner, "-->") || strings.Count(inner, " ") != 2 {
				t.Errorf("QuestionMarker = %q, want one comment whose values hold no space and no comment end", marker)
			}
			got, ok := FindQuestionMarker("Does #277 block #281?\n\n" + marker + "\n")
			if !ok || got != want {
				t.Errorf("FindQuestionMarker = %#v, %v, want %#v", got, ok, want)
			}
		})
	}
	if got := QuestionMarker("blocks", "deps", "crew:deps:ready"); got !=
		"<!-- crew:question id=blocks rule=deps return=crew%3Adeps%3Aready -->" {
		t.Errorf("QuestionMarker = %q", got)
	}
}

// The text of a question may render an issue's title, which anyone may
// write: crew's own marker comes last, so a marker the text holds before it
// never names the question.
func TestTheLastQuestionMarkerOfABodyNamesItsQuestion(t *testing.T) {
	forged := QuestionMarker("blocks", "release", "crew:release:ready")
	body := "Does " + forged + " block?\n\n" + QuestionMarker("blocks", "deps", "crew:deps:ready") + "\n"
	want := PostedQuestion{ID: "blocks", Rule: "deps", Return: "crew:deps:ready"}
	if got, ok := FindQuestionMarker(body); !ok || got != want {
		t.Errorf("FindQuestionMarker = %#v, %v, want %#v", got, ok, want)
	}
	for _, body := range []string{
		"no marker",
		"<!-- crew:question id=blocks rule=deps -->",
		"<!-- crew:question id=blocks rule=deps return=crew%ZZ -->",
		"<!-- crew:question id=blocks rule=deps return=r",
	} {
		if got, ok := FindQuestionMarker(body); ok {
			t.Errorf("FindQuestionMarker(%q) = %#v, want none", body, got)
		}
	}
}

func TestADelegatedMarkerNamesItsQuestion(t *testing.T) {
	for id, want := range map[QuestionID]string{
		"blocks": "<!-- crew:delegated id=blocks -->",
		"":       "<!-- crew:delegated id= -->",
	} {
		marker := DelegatedMarker(id)
		if marker != want {
			t.Errorf("DelegatedMarker(%q) = %q, want %q", id, marker, want)
		}
		if body := "@octocat, please answer.\n\n" + marker + "\n"; !HoldsDelegatedMarker(body) || !HoldsMarker(body) {
			t.Errorf("HoldsDelegatedMarker(%q) = false, want true", body)
		}
	}
	if HoldsDelegatedMarker(QuestionMarker("blocks", "deps", "crew:deps:ready")) {
		t.Errorf("a question's marker holds a delegation's")
	}
}
