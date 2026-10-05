package bots

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"

	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Repo is the GitHub repository of the git repository crew runs in.
type Repo struct {
	// Name is the repository's name, without its owner.
	Name string
	// Owner is the login of the account that owns it.
	Owner string
	// OwnerID is that account's id.
	OwnerID int64
	// Org is whether the owner is an organization rather than a user.
	Org bool
}

// repoReply is the part of gh api repos/{owner}/{repo}'s reply crew reads.
type repoReply struct {
	Name  string `json:"name"`
	Owner struct {
		Login string `json:"login"`
		ID    int64  `json:"id"`
		Type  string `json:"type"`
	} `json:"owner"`
}

// ResolveRepo asks gh, run in root, the repository's root, for the GitHub
// repository it resolves there: gh reads the remotes and the default
// repository, and it reads private repositories. Every failure is an
// EnvError: gh missing, gh logged out, no GitHub remote, or a reply crew
// cannot read.
func ResolveRepo(ctx context.Context, run proc.Runner, root string) (Repo, error) {
	out, err := run(ctx, proc.Command{Name: "gh", Args: []string{"api", "repos/{owner}/{repo}"}, Dir: root})
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Repo{}, envErrorf("crew mates needs the gh CLI, which is not on PATH: %w", err)
		}
		return Repo{}, envErrorf("gh could not resolve the repository's GitHub repository; "+
			"check that gh is logged in (run `gh auth login`) and that the repository has a GitHub remote: %w", err)
	}
	var reply repoReply
	if err := json.Unmarshal(out.Stdout, &reply); err != nil {
		return Repo{}, envErrorf("gh api repos/{owner}/{repo}: unreadable output: %w", err)
	}
	return Repo{
		Name:    reply.Name,
		Owner:   reply.Owner.Login,
		OwnerID: reply.Owner.ID,
		Org:     reply.Owner.Type == "Organization",
	}, nil
}
