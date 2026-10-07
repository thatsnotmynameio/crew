package fakegithub

import (
	"bufio"
	"bytes"
	"hash/fnv"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// postComment answers POST repos/:owner/:repo/issues/:number/comments: a
// new comment, written as the account gh acts as, on the issue or pull
// request.
func postComment(g *GitHub, c *call) apiResult {
	it := g.target(c.api)
	if it == nil {
		return notFound()
	}
	body, _ := c.api.fields[keyBody].(string)
	return apiResult{status: http.StatusCreated, body: g.commentJSON(g.comment(it.number, g.actor(c.inv.Env), body))}
}

// listComments answers GET repos/:owner/:repo/issues/:number/comments:
// every comment on the issue or pull request, oldest first, in one page.
func listComments(g *GitHub, c *call) apiResult {
	it := g.target(c.api)
	if it == nil {
		return notFound()
	}
	list := []any{}
	for _, cm := range g.comments {
		if cm.number == it.number {
			list = append(list, g.commentJSON(cm))
		}
	}
	return apiResult{status: http.StatusOK, body: list}
}

// patchComment answers PATCH repos/:owner/:repo/issues/comments/:id: the
// comment's new body.
func patchComment(g *GitHub, c *call) apiResult {
	id, err := strconv.ParseInt(c.api.params["id"], 10, 64)
	if err != nil || !g.ours(c.api) {
		return notFound()
	}
	for _, cm := range g.comments {
		if cm.id == id {
			cm.body, _ = c.api.fields[keyBody].(string)
			cm.updated = g.now()
			g.touch()
			return apiResult{status: http.StatusOK, body: g.commentJSON(cm)}
		}
	}
	return notFound()
}

// target returns the issue or pull request a's number capture names in
// the repository, nil when there is none.
func (g *GitHub) target(a *apiCall) *item {
	n, err := strconv.Atoi(a.params[keyNumber])
	if err != nil || !g.ours(a) {
		return nil
	}
	return g.items[n]
}

// commentJSON returns cm as GitHub's REST API shows a comment.
func (g *GitHub) commentJSON(cm *comment) object {
	id := strconv.FormatInt(cm.id, 10)
	number := strconv.Itoa(cm.number)
	repo := "/repos/" + g.owner + "/" + g.name
	association := "NONE"
	if strings.EqualFold(cm.author, g.owner) {
		association = "OWNER"
	}
	return object{
		{keyURL, apiBase + repo + "/issues/comments/" + id},
		{keyHTMLURL, g.htmlURL(cm.number) + "#issuecomment-" + id},
		{"issue_url", apiBase + repo + "/issues/" + number},
		{"id", cm.id},
		{"node_id", "IC_" + id},
		{"user", g.user(cm.author)},
		{"created_at", cm.created.Format(time.RFC3339)},
		{"updated_at", cm.updated.Format(time.RFC3339)},
		{"author_association", association},
		{keyBody, cm.body},
	}
}

// htmlURL returns the web address of the issue or pull request number.
func (g *GitHub) htmlURL(number int) string {
	kind := "issues"
	if it := g.items[number]; it != nil && it.pull != nil {
		kind = "pull"
	}
	return htmlBase + "/" + g.owner + "/" + g.name + "/" + kind + "/" + strconv.Itoa(number)
}

// accountID returns the id GitHub gives the account or repository named
// name: always the same for the same name.
func accountID(name string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return int64(h.Sum32())
}

// actor returns the login gh acts as with env: the user that hosts.yml in
// GH_CONFIG_DIR names, as gh reads it, and the viewer without one.
func (g *GitHub) actor(env map[string]string) string {
	dir := env["GH_CONFIG_DIR"]
	if dir == "" {
		return g.viewer
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return g.viewer
	}
	defer root.Close()
	hosts, err := root.ReadFile("hosts.yml")
	if err != nil {
		return g.viewer
	}
	lines := bufio.NewScanner(bytes.NewReader(hosts))
	for lines.Scan() {
		if user, ok := strings.CutPrefix(strings.TrimSpace(lines.Text()), "user:"); ok {
			return strings.TrimSpace(user)
		}
	}
	return g.viewer
}
