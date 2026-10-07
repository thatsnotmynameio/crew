package main

import (
	"context"
	"net/http"

	"github.com/thatsnotmynameio/crew/internal/app"
	"github.com/thatsnotmynameio/crew/internal/bots"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// actingBots returns the app.Options.Bots of the repository at root: it
// makes the configured bots act through internal/bots, with the bots
// stored on this machine and GitHub's API, running gh and git as you
// through run. It reports each of Act's steps on the context it receives,
// the checks' one, so they show in the boot log.
func actingBots(run proc.Runner, root string) func(context.Context, crew.BotName, []crew.BotName) (app.Bots, error) {
	return func(ctx context.Context, def crew.BotName, names []crew.BotName) (app.Bots, error) {
		store, err := bots.DefaultStore()
		if err != nil {
			return app.Bots{}, err
		}
		a, err := bots.Act(ctx, bots.ActOptions{
			Run: run, Store: store, Client: bots.NewClient(bots.DefaultAPI, &http.Client{}),
			Root: root, Names: botNames(names), Default: string(def),
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
		Identities: map[crew.BotName]port.Identity{}, Logins: a.Logins, Warnings: a.Warnings, Close: a.Close,
		Unable:  byBot(a.Unable),
		Failing: func() map[crew.BotName]string { return byBot(a.Failing()) },
	}
	for _, am := range a.Bots {
		bot := crew.BotName(am.Name)
		m.Identities[bot] = port.Identity{Bot: bot, Login: am.Login, Env: am.Env, Unset: am.Unset}
		if am.WriterEnv == nil {
			continue
		}
		name := am.Name
		m.Writer = port.Identity{
			Bot: bot, Login: am.Login, Env: am.WriterEnv, Unset: am.Unset,
			Renew: func(ctx context.Context) error { return a.Renew(ctx, name) },
		}
	}
	return m
}

// botNames returns names as internal/bots spells a bot's name.
func botNames(names []crew.BotName) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = string(n)
	}
	return out
}

// byBot returns m, which internal/bots keys by bot name, keyed by
// crew.BotName; nil when m is nil.
func byBot(m map[string]string) map[crew.BotName]string {
	if m == nil {
		return nil
	}
	out := make(map[crew.BotName]string, len(m))
	for name, v := range m {
		out[crew.BotName(name)] = v
	}
	return out
}
