package main

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/bots"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

func TestAppBotsGivesEachBotItsIdentityAndCrewTheDefaults(t *testing.T) {
	a := &bots.Acting{
		Bots: []bots.ActingBot{
			{Name: "ops", Login: "crew-ops[bot]", Env: []string{"GH_CONFIG_DIR=/run/ops/sessions"},
				Unset: []string{"GH_TOKEN"}, WriterEnv: []string{"GH_CONFIG_DIR=/run/ops/crew"}},
			{Name: "developer", Login: "crew-developer[bot]", Env: []string{"GH_CONFIG_DIR=/run/developer/sessions"},
				Unset: []string{"GH_TOKEN"}},
		},
		Logins:   []string{"crew-ops[bot]", "crew-developer[bot]", "crew-qa[bot]"},
		Warnings: []string{"bot qa has no key on this machine"},
	}
	m := appBots(a)
	ops, dev := m.Identities["ops"], m.Identities["developer"]
	if len(m.Identities) != 2 || ops.Login != "crew-ops[bot]" ||
		!slices.Equal(ops.Env, []string{"GH_CONFIG_DIR=/run/ops/sessions"}) ||
		dev.Login != "crew-developer[bot]" || !slices.Equal(dev.Unset, []string{"GH_TOKEN"}) || ops.Renew != nil {
		t.Errorf("Identities = %+v, want ops and developer acting through their sessions' directories", m.Identities)
	}
	if m.Writer.Bot != "ops" || !slices.Equal(m.Writer.Env, []string{"GH_CONFIG_DIR=/run/ops/crew"}) ||
		!slices.Equal(m.Writer.Unset, []string{"GH_TOKEN"}) {
		t.Errorf("Writer = %+v, want ops through crew's own directory", m.Writer)
	}
	// This Acting minted no token, so the renewal says ops does not act.
	if err := m.Writer.Renew(context.Background()); err == nil || !strings.Contains(err.Error(), "bot ops") {
		t.Errorf("Writer.Renew = %v, want it to renew ops", err)
	}
	if !slices.Equal(m.Warnings, a.Warnings) || !slices.Equal(m.Logins, a.Logins) || m.Close == nil {
		t.Errorf("Warnings = %q, want a's, and a Close", m.Warnings)
	}
	m.Close()
}

func TestAppBotsCopiesTheShortReasonsAndTheRenewalFailures(t *testing.T) {
	a := &bots.Acting{Unable: map[string]string{"qa": "no key", "reviewer": "not installed"}}

	m := appBots(a)

	if !maps.Equal(m.Unable, a.Unable) {
		t.Errorf("Unable = %q, want a's %q", m.Unable, a.Unable)
	}
	if m.Failing == nil {
		t.Fatal("Failing = nil, want a's accessor")
	}
	// No renewal ran, so none failed.
	if failing := m.Failing(); failing == nil || len(failing) != 0 {
		t.Errorf("Failing() = %q, want a's empty reading", failing)
	}
}

func TestActingBotsRefusesAnInvalidName(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	run := func(context.Context, proc.Command) (proc.Output, error) {
		return proc.Output{}, errors.New("nothing may run")
	}
	_, err := actingBots(run, t.TempDir())(context.Background(), "Ops", []string{"Ops"})
	if _, ok := errors.AsType[*bots.EnvError](err); !ok {
		t.Errorf("actingBots = %v, want an EnvError", err)
	}
}

func TestActingBotsReportsItsStepsOnTheChecksContext(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	run := func(context.Context, proc.Command) (proc.Output, error) {
		return proc.Output{}, errors.New("gh: not logged in")
	}
	var steps []string
	ctx := port.WithSteps(context.Background(), func(step string) { steps = append(steps, step) })
	if _, err := actingBots(run, t.TempDir())(ctx, "ops", []string{"ops"}); err == nil {
		t.Fatal("actingBots = nil, want gh's failure")
	}
	if want := []string{"resolving the repository for the bots"}; !slices.Equal(steps, want) {
		t.Errorf("steps = %q, want %q", steps, want)
	}
}

func TestActingBotsWithoutNamesActsAsNone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m, err := actingBots(nil, t.TempDir())(context.Background(), "", nil)
	if err != nil || len(m.Identities) != 0 || m.Writer.Login != "" {
		t.Fatalf("actingBots = %+v, %v; want no bot", m, err)
	}
	m.Close()
}
