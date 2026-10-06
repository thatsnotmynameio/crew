package fakegithub

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Invocation is one run of gh.
type Invocation struct {
	// Args is gh's argument list, without the leading "gh".
	Args []string
	// Stdin is what gh's standard input held.
	Stdin []byte
	// Env holds gh's environment variables that matter to GitHub, such as
	// GH_CONFIG_DIR, whose hosts.yml names the account gh writes as.
	Env map[string]string
}

// Reply is what gh printed and how it exited.
type Reply struct {
	// Stdout and Stderr are what gh printed on its standard output and
	// standard error.
	Stdout, Stderr []byte
	// Code is gh's exit code.
	Code int
	// Violation is "" for a call the fake knows. For one it does not know,
	// it is the call as a quoted command line followed by what the fake did
	// not know, in parentheses; Code is then 1 and Stderr says the same.
	Violation string
}

// command is a gh subcommand the fake knows: the flags it accepts, a check
// of the call that returns what the fake does not know about it, "" when it
// knows everything, and the handler that answers it.
type command struct {
	flags flagSpec
	// url is the address gh names in this command's HTTP errors.
	url   string
	check func(g *GitHub, c *call) string
	run   func(g *GitHub, c *call) Reply
}

// call is one gh call being answered.
type call struct {
	inv   Invocation
	words []string // the arguments, {owner} and {repo} filled in
	cmd   command
	flags map[string][]string
	args  []string
	api   *apiCall // the parsed gh api call; nil for other commands
}

// flag returns the last value of the flag name, "" when it is not set.
func (c *call) flag(name string) string {
	v := c.flags[name]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

// has reports whether the flag name was given.
func (c *call) has(name string) bool {
	_, ok := c.flags[name]
	return ok
}

// Run answers one gh call from the repository's state, changing it as the
// call does.
func (g *GitHub) Run(inv Invocation) Reply {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.log = append(g.log, quote(inv.Args))
	c := &call{inv: inv, words: make([]string, len(inv.Args))}
	for i, a := range inv.Args {
		c.words[i] = g.resolve(a)
	}
	cmd, rest, ok := g.lookup(inv.Args)
	if !ok {
		return violation(inv.Args, "unknown command")
	}
	c.cmd = cmd
	flags, args, err := cmd.flags.parse(rest)
	if err != "" {
		return violation(inv.Args, err)
	}
	c.flags, c.args = flags, args
	if cmd.check != nil {
		if reason := cmd.check(g, c); reason != "" {
			return violation(inv.Args, reason)
		}
	}
	if r, ok := g.scripted(c); ok {
		return r
	}
	return cmd.run(g, c)
}

// lookup returns the command args name, by its two first words or its
// first, and the arguments after those words.
func (g *GitHub) lookup(args []string) (command, []string, bool) {
	const two = 2
	if len(args) >= two {
		if cmd, ok := g.commands[args[0]+" "+args[1]]; ok {
			return cmd, args[two:], true
		}
	}
	if len(args) >= 1 {
		if cmd, ok := g.commands[args[0]]; ok {
			return cmd, args[1:], true
		}
	}
	return command{}, nil, false
}

// scripted returns the GitHub error a Fail scripted for c, and whether one
// did, using up one of its times.
func (g *GitHub) scripted(c *call) (Reply, bool) {
	for _, f := range g.failures {
		if f.left > 0 && subsequence(c.words, f.words) {
			f.left--
			if c.api != nil {
				return apiError(f.status, f.message), true
			}
			return failed(fmt.Sprintf("HTTP %d: %s (%s)", f.status, f.message, g.resolve(c.cmd.url))), true
		}
	}
	return Reply{}, false
}

// subsequence reports whether words holds want in order, not necessarily
// next to each other.
func subsequence(words, want []string) bool {
	i := 0
	for _, w := range words {
		if i < len(want) && w == want[i] {
			i++
		}
	}
	return i == len(want)
}

// violation returns the reply to a call the fake does not know: why, as
// reason, after the call.
func violation(args []string, reason string) Reply {
	v := quote(args) + " (" + reason + ")"
	return Reply{Stderr: []byte("acceptance: unknown gh call: " + v + "\n"), Code: 1, Violation: v}
}

// failed returns the reply of a gh call that failed with message.
func failed(message string) Reply {
	return Reply{Stderr: []byte(message + "\n"), Code: 1}
}

// printed returns the reply of a gh call that succeeded printing out.
func printed(out string) Reply {
	return Reply{Stdout: []byte(out)}
}

// quote returns gh's command line for args, as CommandLine writes it.
func quote(args []string) string {
	return CommandLine("gh", args)
}

// CommandLine returns the command line that runs the program name with
// args, each argument quoted for a POSIX shell where it needs it, and any
// longer than 200 characters cut with its length shown. Violations name
// calls this way.
func CommandLine(name string, args []string) string {
	words := make([]string, 0, len(args)+1)
	words = append(words, name)
	for _, a := range args {
		words = append(words, shellQuote(shorten(a)))
	}
	return strings.Join(words, " ")
}

// shorten cuts s after 200 characters and shows its length.
func shorten(s string) string {
	const limit = 200
	n := utf8.RuneCountInString(s)
	if n <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + fmt.Sprintf("…(%d chars)", n)
}

// shellQuote quotes s for a POSIX shell, unless it is made only of
// characters a shell reads as themselves.
func shellQuote(s string) string {
	plain := func(r rune) bool {
		return r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("@%+=:,./_-", r))
	}
	if s != "" && !slices.ContainsFunc([]rune(s), func(r rune) bool { return !plain(r) }) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// flagSpec lists the flags a command accepts: each spelling, short or
// long, to the flag's long name, and whether that flag takes a value.
type flagSpec struct {
	names  map[string]string
	valued map[string]bool
}

// parse splits args into the flags spec knows, by long name, each with its
// values in order, and the positional arguments. It returns why it cannot,
// "" when it can.
func (s flagSpec) parse(args []string) (map[string][]string, []string, string) {
	flags := map[string][]string{}
	var positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}
		spelled, value, hasValue := strings.Cut(a, "=")
		name, ok := s.names[spelled]
		if !ok {
			return nil, nil, "unknown flag " + spelled
		}
		switch {
		case !s.valued[name] && hasValue:
			return nil, nil, "flag " + spelled + " takes no value"
		case !s.valued[name]:
		case !hasValue && i+1 >= len(args):
			return nil, nil, "flag " + spelled + " needs a value"
		case !hasValue:
			i++
			value = args[i]
		}
		flags[name] = append(flags[name], value)
	}
	return flags, positional, ""
}

// spec builds a flagSpec from flags, each a long name (as "--json") with
// its short spelling after a space when it has one (as "--jq -q"), and a
// trailing "=" on the long name when it takes a value (as "--json=").
func spec(flags ...string) flagSpec {
	s := flagSpec{names: map[string]string{}, valued: map[string]bool{}}
	for _, f := range flags {
		spellings := strings.Fields(f)
		name, valued := strings.CutSuffix(spellings[0], "=")
		s.valued[name] = valued
		s.names[name] = name
		for _, short := range spellings[1:] {
			s.names[short] = name
		}
	}
	return s
}
