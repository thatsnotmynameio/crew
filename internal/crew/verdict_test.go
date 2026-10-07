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
