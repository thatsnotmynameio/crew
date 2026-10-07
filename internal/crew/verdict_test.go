package crew

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseVerdict(t *testing.T) {
	long := "a" + strings.Repeat("b", 63)
	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"a word", "blocked", true},
		{"an underscore", "needs_person", true},
		{"a dash", "no-pr", true},
		{"a digit after the first letter", "x1", true},
		{"a built-in verdict", "passed", true},
		{"64 characters", long, true},
		{"an uppercase letter", "Blocked", false},
		{"a leading digit", "1x", false},
		{"empty", "", false},
		{"65 characters", long + "c", false},
		{"a space", "no pr", false},
		{"a leading dash", "-x", false},
		{"a non-ASCII letter", "não", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := ParseVerdict(tt.in)
			if tt.ok && (err != nil || v != Verdict(tt.in)) {
				t.Fatalf("ParseVerdict(%q) = %q, %v; want %q, nil", tt.in, v, err, tt.in)
			}
			if !tt.ok && (err == nil || v != "") {
				t.Fatalf("ParseVerdict(%q) = %q, %v; want an error", tt.in, v, err)
			}
		})
	}
}

func TestParseVerdictErrorNamesTheName(t *testing.T) {
	_, err := ParseVerdict("Blocked")
	if err == nil || !strings.Contains(err.Error(), `"Blocked"`) {
		t.Fatalf("ParseVerdict(%q) = %v, want an error naming it", "Blocked", err)
	}
}

func TestParseRouteName(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{"needs-person", true},
		{"failed", true},
		{"next", false},
		{"Needs-Person", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r, err := ParseRouteName(tt.in)
			if tt.ok && (err != nil || r != RouteName(tt.in)) {
				t.Fatalf("ParseRouteName(%q) = %q, %v; want %q, nil", tt.in, r, err, tt.in)
			}
			if !tt.ok && (err == nil || r != "") {
				t.Fatalf("ParseRouteName(%q) = %q, %v; want an error", tt.in, r, err)
			}
		})
	}
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		in   string
		want Target
		ok   bool
	}{
		{"next", Next{}, true},
		{"blocked", ToRoute{Route: "blocked"}, true},
		{"failed", ToRoute{Route: FailedRoute}, true},
		{"Blocked", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseTarget(tt.in)
			if tt.ok && (err != nil || got != tt.want) {
				t.Fatalf("ParseTarget(%q) = %v, %v; want %v, nil", tt.in, got, err, tt.want)
			}
			if !tt.ok && (err == nil || got != nil) {
				t.Fatalf("ParseTarget(%q) = %v, %v; want an error", tt.in, got, err)
			}
		})
	}
}

func TestOnTarget(t *testing.T) {
	tests := []struct {
		name    string
		on      On
		verdict Verdict
		want    Target
	}{
		{"passed with no entry goes next", nil, Passed, Next{}},
		{"failed with no entry takes the failed route", nil, Failed, ToRoute{Route: FailedRoute}},
		{"waiting with no entry takes the failed route", nil, Waiting, ToRoute{Route: FailedRoute}},
		{"another verdict with no entry takes the failed route", nil, "blocked", ToRoute{Route: FailedRoute}},
		{
			"an entry names the verdict's route",
			On{"blocked": ToRoute{Route: "blocked"}}, "blocked", ToRoute{Route: "blocked"},
		},
		{
			"an entry for another verdict leaves passed going next",
			On{"blocked": ToRoute{Route: "blocked"}}, Passed, Next{},
		},
		{
			"an entry can send passed to a route",
			On{Passed: ToRoute{Route: "no-pr"}}, Passed, ToRoute{Route: "no-pr"},
		},
		{
			"an entry can send failed on to the next action",
			On{Failed: Next{}}, Failed, Next{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.on.Target(tt.verdict); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("On.Target(%q) = %#v, want %#v", tt.verdict, got, tt.want)
			}
		})
	}
}

// blockedOn maps the verdicts blocked and needs_person to routes of their
// own.
var blockedOn = On{"blocked": ToRoute{Route: "blocked"}, "needs_person": ToRoute{Route: "needs-person"}}

var done, gaveUp = succeeded("done"), failedOutcome("gave up")

// sessionJudgments are the verdicts of lfg's sessions, with blockedOn its
// targets.
var sessionJudgments = []struct {
	name     string
	outcome  Outcome
	report   VerdictReport
	stopping bool
	want     Judged
}{
	{
		name: "a session that succeeded with no report passes", outcome: done, report: NoVerdictReported{},
		want: Judged{Verdict: Passed, End: EndSucceeded{Reason: NewSessionText("done")}},
	},
	{
		name: "AE3: a reported verdict its on names is its verdict", outcome: done,
		report: VerdictReported{Verdict: "blocked"},
		want:   Judged{Verdict: "blocked", End: EndSucceeded{Reason: NewSessionText("done")}},
	},
	{
		name: "a built-in verdict its on does not name is its verdict", outcome: done,
		report: VerdictReported{Verdict: Waiting},
		want:   Judged{Verdict: Waiting, End: EndSucceeded{Reason: NewSessionText("done")}},
	},
	{
		name: "a session that reports failed fails by its session", outcome: done,
		report: VerdictReported{Verdict: Failed},
		want:   Judged{Verdict: Failed, End: EndFailed{Reason: NewSessionText("done"), Cause: CauseSession}},
	},
	{
		name: "AE3: a reported verdict its on does not name fails, in crew's words", outcome: done,
		report: VerdictReported{Verdict: "too-big"},
		want: Judged{Verdict: Failed, End: EndFailed{
			Reason: NewSessionText(`the session reported the verdict "too-big", which its on: does not name`),
			Cause:  CauseVerdict,
		}},
	},
	{
		name: "a report with no verdict name fails, in crew's words", outcome: done, report: VerdictUnreadable{},
		want: Judged{Verdict: Failed, End: EndFailed{
			Reason: NewSessionText("the session's verdict holds no verdict name"), Cause: CauseVerdict,
		}},
	},
	{
		name: "a failed harness fails whatever the session reported", outcome: gaveUp,
		report: VerdictReported{Verdict: "blocked"},
		want:   Judged{Verdict: Failed, End: EndFailed{Reason: NewSessionText("gave up"), Cause: CauseSession}},
	},
	{
		name: "a stop fails a session that succeeded, whatever it reported", outcome: done,
		report: VerdictReported{Verdict: "blocked"}, stopping: true,
		want: Judged{Verdict: Failed, End: EndFailed{Reason: NewSessionText("done"), Cause: CauseStopped}},
	},
	{
		name: "a stop fails a session that failed, as stopped", outcome: gaveUp, report: NoVerdictReported{},
		stopping: true,
		want:     Judged{Verdict: Failed, End: EndFailed{Reason: NewSessionText("gave up"), Cause: CauseStopped}},
	},
}

func TestJudgeSession(t *testing.T) {
	for _, tt := range sessionJudgments {
		t.Run(tt.name, func(t *testing.T) {
			if got := judgeSession(blockedOn, tt.outcome, tt.report, tt.stopping); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("judgeSession = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// shellJudgments are the verdicts of a judge script whose exit statuses 3,
// 4 and 5 give needs_person, too-big and passed, with blockedOn its
// targets.
var shellJudgments = []struct {
	name     string
	outcome  ShellOutcome
	stopping bool
	want     Judged
}{
	{
		name: "exit 0 passes", outcome: exited(0, "judge passed: ok"),
		want: Judged{Verdict: Passed, End: EndSucceeded{Reason: NewSessionText("judge passed: ok")}},
	},
	{
		name: "AE1: any other exit fails by its script", outcome: exited(2, "judge failed: red"),
		want: Judged{Verdict: Failed, End: EndFailed{Reason: NewSessionText("judge failed: red"), Cause: CauseShell}},
	},
	{
		name: "a signal fails by its script", outcome: exited(-1, "judge was killed"),
		want: Judged{Verdict: Failed, End: EndFailed{Reason: NewSessionText("judge was killed"), Cause: CauseShell}},
	},
	{
		name: "AE2: an exit its verdicts name gives that verdict", outcome: exited(3, "judge: ask"),
		want: Judged{Verdict: "needs_person", End: EndSucceeded{Reason: NewSessionText("judge: ask")}},
	},
	{
		name: "an exit its verdicts name passed passes", outcome: exited(5, "judge: fine"),
		want: Judged{Verdict: Passed, End: EndSucceeded{Reason: NewSessionText("judge: fine")}},
	},
	{
		name: "a verdict its on does not name fails, in crew's words", outcome: exited(4, "judge: big"),
		want: Judged{Verdict: Failed, End: EndFailed{
			Reason: NewSessionText(`the script's exit status 4 gives the verdict "too-big", which its on: does not name`),
			Cause:  CauseVerdict,
		}},
	},
	{
		name:    "a script that did not run to its end fails by its script",
		outcome: ShellOutcome{Reason: NewShellReason("judge could not start")},
		want: Judged{Verdict: Failed, End: EndFailed{
			Reason: NewSessionText("judge could not start"), Cause: CauseShell,
		}},
	},
	{
		name: "a stop fails a script, whatever it exited with", outcome: exited(3, "judge was stopped"), stopping: true,
		want: Judged{Verdict: Failed, End: EndFailed{Reason: NewSessionText("judge was stopped"), Cause: CauseStopped}},
	},
}

func TestJudgeShell(t *testing.T) {
	judge := ShellSpec{Script: "./judge", Verdicts: map[int]Verdict{3: "needs_person", 4: "too-big", 5: Passed}}
	for _, tt := range shellJudgments {
		t.Run(tt.name, func(t *testing.T) {
			if got := judgeShell(judge, blockedOn, tt.outcome, tt.stopping); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("judgeShell = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// Covers R19: only a session whose on: has a waiting entry may wait for an
// answer; a shell action never does, whatever its on: says.
func TestActionMayWait(t *testing.T) {
	waiting := On{Waiting: ToRoute{Route: "waiting"}}
	tests := []struct {
		name   string
		action Action
		want   bool
	}{
		{"a session with a waiting entry", Action{Kind: SessionSpec{}, On: waiting}, true},
		{"a session with other entries only", Action{Kind: SessionSpec{}, On: On{"blocked": Next{}}}, false},
		{"a session with no on", Action{Kind: SessionSpec{}}, false},
		{"a shell action with a waiting entry", Action{Kind: ShellSpec{}, On: waiting}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.action.MayWait(); got != tt.want {
				t.Errorf("MayWait() = %v, want %v", got, tt.want)
			}
		})
	}
}
