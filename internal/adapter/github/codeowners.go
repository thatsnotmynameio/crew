package github

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

// rawAccept asks GitHub's contents API for a file's raw text.
const rawAccept = "Accept: application/vnd.github.raw+json"

// findBoss returns the boss's logins (KTD5): every user the catch-all `*`
// rule of the repository's CODEOWNERS names, its teams expanded to their
// members, each once, ignoring case. With no CODEOWNERS, or a `*` rule that
// names no user, the boss is gh's login.
func (t *Tracker) findBoss(ctx context.Context) ([]string, error) {
	login, err := t.gh.viewer(ctx)
	if err != nil {
		return nil, err
	}
	text, err := t.codeowners(ctx)
	if err != nil {
		return nil, err
	}
	var boss []string
	for _, owner := range catchAllOwners(text) {
		logins := []string{owner}
		if org, team, ok := strings.Cut(owner, "/"); ok {
			if logins, err = t.teamMembers(ctx, org, team); err != nil {
				return nil, err
			}
		}
		for _, l := range logins {
			if !containsFold(boss, l) {
				boss = append(boss, l)
			}
		}
	}
	if len(boss) == 0 {
		return []string{login}, nil
	}
	return boss, nil
}

// codeowners returns the text of the repository's CODEOWNERS on its default
// branch, read where GitHub looks for it and in its order, or "" when there
// is none.
func (t *Tracker) codeowners(ctx context.Context) (string, error) {
	for _, path := range []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"} {
		out, err := t.gh.call(ctx, "api", "-H", rawAccept, "repos/{owner}/{repo}/contents/"+path)
		if err == nil {
			return string(out.Stdout), nil
		}
		if httpStatus(string(out.Stderr)) != http.StatusNotFound {
			return "", fmt.Errorf("read %s: %w", path, err)
		}
	}
	return "", nil
}

// teamMembers returns the logins of the members of org's team, whose slug
// is team.
func (t *Tracker) teamMembers(ctx context.Context, org, team string) ([]string, error) {
	out, err := t.gh.call(ctx, "api", "--paginate", "orgs/"+org+"/teams/"+team+"/members", "--jq", ".[].login")
	if err != nil {
		return nil, fmt.Errorf("list the members of the team @%s/%s that CODEOWNERS names; "+
			"if gh lacks the read:org scope, run `gh auth refresh -s read:org`: %w", org, team, err)
	}
	return strings.Fields(string(out.Stdout)), nil
}

// catchAllOwners returns the users and teams, as login or org/team, that the
// last `*` rule of the CODEOWNERS text names, since a later rule wins.
// Email owners are skipped, and so is everything after a #.
func catchAllOwners(text string) []string {
	var owners []string
	for line := range strings.Lines(text) {
		fields := strings.Fields(line)
		if i := slices.IndexFunc(fields, func(f string) bool { return strings.HasPrefix(f, "#") }); i >= 0 {
			fields = fields[:i]
		}
		if len(fields) == 0 || fields[0] != "*" {
			continue
		}
		owners = owners[:0]
		for _, f := range fields[1:] {
			if name, ok := strings.CutPrefix(f, "@"); ok && name != "" {
				owners = append(owners, name)
			}
		}
	}
	return owners
}

// containsFold reports whether logins holds login, ignoring case, as GitHub
// compares logins.
func containsFold(logins []string, login string) bool {
	return slices.ContainsFunc(logins, func(l string) bool { return strings.EqualFold(l, login) })
}
