package crew

// Agent is one of the config's agents: what runs the sessions of the
// actions that name it. Its harness's own settings, the model included,
// belong to the harness adapter, not to crew.
type Agent struct {
	// Name identifies the agent, as the config's agents key it.
	Name AgentName
	// Harness is the name of the harness adapter that runs its sessions.
	Harness HarnessName
	// Bot is the bot the agent names, which its actions act as; empty when
	// it names none, and its actions act as the tracker's bot.
	Bot BotName
}

// Bot is one of crew's bots: an identity only, which carries no model,
// prompt or settings. Its credentials are the runtime's, keyed by its
// name, and never part of the domain. The zero Bot is you: the gh login
// crew runs as.
type Bot struct {
	// Name identifies the bot, as the config names it.
	Name BotName
}
