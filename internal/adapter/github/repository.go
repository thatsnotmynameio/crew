package github

import (
	"context"
	"errors"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// repositoryQuery reads the repository's node id, which survives a rename,
// and its owner/name.
const repositoryQuery = `query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) { id nameWithOwner }
}`

// readRepository reads, as you, the repository gh resolves from crew's
// working directory, with owner and name filled as pullRequests fills them,
// and keeps it for Repository. A reply without an id is an error.
func (t *Tracker) readRepository(ctx context.Context) error {
	var reply struct {
		Data struct {
			Repository struct {
				ID            string `json:"id"`
				NameWithOwner string `json:"nameWithOwner"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := t.gh.decode(ctx, &reply, "api", "graphql", "-f", "query="+repositoryQuery,
		"-F", "owner={owner}", "-F", "name={repo}"); err != nil {
		return err
	}
	r := reply.Data.Repository
	if r.ID == "" {
		return errors.New("gh api graphql printed no id")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.repository = crew.Repository{ID: crew.RepositoryID(r.ID), Name: r.NameWithOwner}
	return nil
}

// Repository implements port.RepositoryFinder: the repository's node id and
// owner/name, as Prepare found them, the zero Repository before.
func (t *Tracker) Repository() crew.Repository {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.repository
}
