package main

import (
	"context"
	"net/http"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/bots"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// actingBots returns the app.Options.Bots of the repository at root: it
// makes the configured bots act through internal/bots, with the bots
// stored on this machine and GitHub's API, running gh and git as you
// through run. It reports each of Act's steps on the context it receives,
// the checks' one, so they show in the boot log.
func actingBots(run proc.Runner, root string) func(context.Context, string, []string) (app.Bots, error) {
	return func(ctx context.Context, def string, names []string) (app.Bots, error) {
		store, err := bots.DefaultStore()
		if err != nil {
			return app.Bots{}, err
		}
		a, err := bots.Act(ctx, bots.ActOptions{
			Run: run, Store: store, Client: bots.NewClient(bots.DefaultAPI, &http.Client{}),
			Root: root, Names: names, Default: def,
			Step: func(step string) { port.Step(ctx, step) },
		})
		if err != nil {
			return app.Bots{}, err
		}
		return appBots(a), nil
	}
}

// appBots returns the bots acting in a as app.Bots: each one's identity
// for its sessions and checks, and the default bot's for crew's own
// writes, which renews its token through a, with the short reason of each
// bot that cannot act and a's renewal failures. None holds a key or a
// token, only the gh config directory that holds the token.
func appBots(a *bots.Acting) app.Bots {
	m := app.Bots{
		Identities: map[string]port.Identity{}, Logins: a.Logins, Warnings: a.Warnings, Close: a.Close,
		Unable: a.Unable, Failing: a.Failing,
	}
	for _, am := range a.Bots {
		m.Identities[am.Name] = port.Identity{Bot: am.Name, Login: am.Login, Env: am.Env, Unset: am.Unset}
		if am.WriterEnv == nil {
			continue
		}
		name := am.Name
		m.Writer = port.Identity{
			Bot: am.Name, Login: am.Login, Env: am.WriterEnv, Unset: am.Unset,
			Renew: func(ctx context.Context) error { return a.Renew(ctx, name) },
		}
	}
	return m
}
