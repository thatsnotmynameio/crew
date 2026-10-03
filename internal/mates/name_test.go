package mates

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckNameAcceptsValidNames(t *testing.T) {
	for _, name := range []string{"tester", "dev", "a", "qa-2", strings.Repeat("a", 29)} {
		if err := CheckName(name); err != nil {
			t.Errorf("CheckName(%q) = %v, want nil", name, err)
		}
	}
}

func TestAppNameIsCrewDashName(t *testing.T) {
	if got := AppName("tester"); got != "crew-tester" {
		t.Errorf("AppName(tester) = %q, want crew-tester", got)
	}
}

func TestCheckNameSaysWhichRuleFailed(t *testing.T) {
	for name, want := range map[string]string{
		"":                      "empty",
		strings.Repeat("a", 30): "the limit is 29",
		"Tester":                "only lowercase letters, digits and hyphens",
		"te_ster":               "only lowercase letters, digits and hyphens",
		"te ster":               "only lowercase letters, digits and hyphens",
		"-dev":                  "starts with a hyphen",
		"dev-":                  "ends with a hyphen",
		"de--v":                 "two hyphens in a row",
	} {
		err := CheckName(name)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CheckName(%q) = %v, want an error saying %q", name, err, want)
		}
		if name != "" && err != nil && !strings.Contains(err.Error(), name) {
			t.Errorf("CheckName(%q) = %v, want the name in the message", name, err)
		}
		if _, ok := errors.AsType[*EnvError](err); !ok {
			t.Errorf("CheckName(%q) = %v, want an *EnvError", name, err)
		}
	}
}
