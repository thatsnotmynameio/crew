package main

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/mates"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

func TestAppMatesGivesEachMateItsIdentityAndCrewTheDefaults(t *testing.T) {
	a := &mates.Acting{
		Mates: []mates.ActingMate{
			{Name: "ops", Login: "crew-ops[bot]", Env: []string{"GH_CONFIG_DIR=/run/ops/sessions"},
				Unset: []string{"GH_TOKEN"}, WriterEnv: []string{"GH_CONFIG_DIR=/run/ops/crew"}},
			{Name: "developer", Login: "crew-developer[bot]", Env: []string{"GH_CONFIG_DIR=/run/developer/sessions"},
				Unset: []string{"GH_TOKEN"}},
		},
		Logins:   []string{"crew-ops[bot]", "crew-developer[bot]", "crew-qa[bot]"},
		Warnings: []string{"mate qa has no key on this machine"},
	}
	m := appMates(a)
	ops, dev := m.Identities["ops"], m.Identities["developer"]
	if len(m.Identities) != 2 || ops.Login != "crew-ops[bot]" ||
		!slices.Equal(ops.Env, []string{"GH_CONFIG_DIR=/run/ops/sessions"}) ||
		dev.Login != "crew-developer[bot]" || !slices.Equal(dev.Unset, []string{"GH_TOKEN"}) || ops.Renew != nil {
		t.Errorf("Identities = %+v, want ops and developer acting through their sessions' directories", m.Identities)
	}
	if m.Writer.Mate != "ops" || !slices.Equal(m.Writer.Env, []string{"GH_CONFIG_DIR=/run/ops/crew"}) ||
		!slices.Equal(m.Writer.Unset, []string{"GH_TOKEN"}) {
		t.Errorf("Writer = %+v, want ops through crew's own directory", m.Writer)
	}
	// This Acting minted no token, so the renewal says ops does not act.
	if err := m.Writer.Renew(context.Background()); err == nil || !strings.Contains(err.Error(), "mate ops") {
		t.Errorf("Writer.Renew = %v, want it to renew ops", err)
	}
	if !slices.Equal(m.Warnings, a.Warnings) || !slices.Equal(m.Logins, a.Logins) || m.Close == nil {
		t.Errorf("Warnings = %q, want a's, and a Close", m.Warnings)
	}
	m.Close()
}

func TestActingMatesRefusesAnInvalidName(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	run := func(context.Context, proc.Command) (proc.Output, error) {
		return proc.Output{}, errors.New("nothing may run")
	}
	_, err := actingMates(run, t.TempDir())(context.Background(), "Ops", []string{"Ops"})
	if _, ok := errors.AsType[*mates.EnvError](err); !ok {
		t.Errorf("actingMates = %v, want an EnvError", err)
	}
}

func TestActingMatesWithoutNamesActsAsNone(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m, err := actingMates(nil, t.TempDir())(context.Background(), "", nil)
	if err != nil || len(m.Identities) != 0 || m.Writer.Login != "" {
		t.Fatalf("actingMates = %+v, %v; want no mate", m, err)
	}
	m.Close()
}
