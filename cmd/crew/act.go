package main

import (
	"context"
	"net/http"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/mates"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// actingMates returns the app.Options.Mates of the repository at root: it
// makes the configured mates act through internal/mates, with the mates
// stored on this machine and GitHub's API, running gh and git as the boss
// through run. It reports each of Act's steps on the context it receives,
// the checks' one, so they show in the boot log.
func actingMates(run proc.Runner, root string) func(context.Context, string, []string) (app.Mates, error) {
	return func(ctx context.Context, def string, names []string) (app.Mates, error) {
		store, err := mates.DefaultStore()
		if err != nil {
			return app.Mates{}, err
		}
		a, err := mates.Act(ctx, mates.ActOptions{
			Run: run, Store: store, Client: mates.NewClient(mates.DefaultAPI, &http.Client{}),
			Root: root, Names: names, Default: def,
			Step: func(step string) { port.Step(ctx, step) },
		})
		if err != nil {
			return app.Mates{}, err
		}
		return appMates(a), nil
	}
}

// appMates returns the mates acting in a as app.Mates: each one's identity
// for its sessions and checks, and the default mate's for crew's own
// writes, which renews its token through a. None holds a key or a token,
// only the gh config directory that holds the token.
func appMates(a *mates.Acting) app.Mates {
	m := app.Mates{Identities: map[string]port.Identity{}, Logins: a.Logins, Warnings: a.Warnings, Close: a.Close}
	for _, am := range a.Mates {
		m.Identities[am.Name] = port.Identity{Mate: am.Name, Login: am.Login, Env: am.Env, Unset: am.Unset}
		if am.WriterEnv == nil {
			continue
		}
		name := am.Name
		m.Writer = port.Identity{
			Mate: am.Name, Login: am.Login, Env: am.WriterEnv, Unset: am.Unset,
			Renew: func(ctx context.Context) error { return a.Renew(ctx, name) },
		}
	}
	return m
}
