package fakegithub

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// The addresses GitHub's replies link to.
const (
	apiBase  = "https://api.github.com"
	htmlBase = "https://github.com"
)

// rawContent is the Accept header that asks the contents API for a file's
// raw text.
const rawContent = "Accept: application/vnd.github.raw+json"

// apiCall is a gh api call, parsed.
type apiCall struct {
	method string
	path   string // the endpoint without its query, placeholders filled
	query  url.Values
	raw    bool           // whether it asks for raw content
	fields map[string]any // the -f and -F fields; a key[] field is a list
	route  route
	params map[string]string  // the route's captures, by name
	doc    *ast.QueryDocument // a GraphQL call's query
}

// route is a REST endpoint the fake knows.
type route struct {
	method string
	// pattern is the path, with ":name" capturing one segment and "*name"
	// the rest of the path.
	pattern  string
	query    []string // the query parameters it accepts
	body     []string // the fields it accepts
	raw      bool     // whether it serves raw content
	paginate bool     // whether it lists, so --paginate applies
	handle   func(g *GitHub, c *call) apiResult
}

// apiResult is GitHub's answer to a REST call: a status, and a JSON body,
// raw text or an error message.
type apiResult struct {
	status  int
	body    any
	text    string
	message string
}

// notFound is GitHub's answer for a missing object.
func notFound() apiResult {
	return apiResult{status: http.StatusNotFound, message: "Not Found"}
}

// routes returns the REST endpoints the fake knows.
func routes() []route {
	return []route{
		{method: http.MethodGet, pattern: "user", handle: getUser},
		{method: http.MethodGet, pattern: "repos/:owner/:repo", handle: getRepo},
		{method: http.MethodGet, pattern: "repos/:owner/:repo/contents/*path", raw: true, handle: getContents},
		{method: http.MethodGet, pattern: "orgs/:org/teams/:team/members", paginate: true, handle: getMembers},
		{method: http.MethodPost, pattern: "repos/:owner/:repo/issues/:number/comments", body: []string{keyBody},
			handle: postComment},
		{method: http.MethodGet, pattern: "repos/:owner/:repo/issues/:number/comments", query: []string{"per_page"},
			paginate: true, handle: listComments},
		{method: http.MethodPatch, pattern: "repos/:owner/:repo/issues/comments/:id", body: []string{keyBody},
			handle: patchComment},
	}
}

// apiCommand is gh api.
func apiCommand() command {
	return command{
		flags: spec("--method= -X", "--header= -H", "--raw-field= -f", "--field= -F", "--jq= -q", "--paginate"),
		check: checkAPI,
		run:   runAPI,
	}
}

// checkAPI parses a gh api call and returns what the fake does not know
// about it.
func checkAPI(g *GitHub, c *call) string {
	if len(c.args) != 1 {
		return "gh api takes one endpoint"
	}
	path, rawQuery, _ := strings.Cut(g.resolve(c.args[0]), "?")
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "unreadable query " + rawQuery
	}
	a := &apiCall{path: path, query: query, fields: g.fields(c)}
	a.method = strings.ToUpper(c.flag("--method"))
	if a.method == "" {
		a.method = http.MethodGet
		if len(a.fields) > 0 {
			a.method = http.MethodPost
		}
	}
	for _, h := range c.flags["--header"] {
		if h != rawContent {
			return "unknown header " + h
		}
		a.raw = true
	}
	c.api = a
	if path == "graphql" {
		return checkGraphQL(a, c)
	}
	return checkRoute(a, c)
}

// checkRoute finds a's route and returns what it does not accept in c.
func checkRoute(a *apiCall, c *call) string {
	i := slices.IndexFunc(routes(), func(r route) bool {
		params, ok := match(r.pattern, a.path)
		a.params = params
		return ok && r.method == a.method
	})
	if i < 0 {
		return "unknown endpoint " + a.method + " " + a.path
	}
	a.route = routes()[i]
	for k := range a.query {
		if !slices.Contains(a.route.query, k) {
			return "unknown query parameter " + k
		}
	}
	for k := range a.fields {
		if !slices.Contains(a.route.body, k) {
			return "unknown field " + k
		}
	}
	switch {
	case a.raw && !a.route.raw:
		return "raw content of " + a.path
	case c.has("--paginate") && !a.route.paginate:
		return "--paginate on " + a.path
	}
	return ""
}

// match returns the captures of path against pattern, and whether it
// matches.
func match(pattern, path string) (map[string]string, bool) {
	want, got := strings.Split(pattern, "/"), strings.Split(path, "/")
	params := map[string]string{}
	for i, w := range want {
		if name, ok := strings.CutPrefix(w, "*"); ok && i < len(got) {
			params[name] = strings.Join(got[i:], "/")
			return params, true
		}
		if i >= len(got) {
			return nil, false
		}
		if name, ok := strings.CutPrefix(w, ":"); ok && got[i] != "" {
			params[name] = got[i]
			continue
		}
		if w != got[i] {
			return nil, false
		}
	}
	return params, len(want) == len(got)
}

// fields returns the -f fields of c, as strings, and its -F fields, typed
// as gh types them; a key ending in [] gathers its values in a list.
func (g *GitHub) fields(c *call) map[string]any {
	fields := map[string]any{}
	add := func(kv string, value func(string) any) {
		k, v, _ := strings.Cut(kv, "=")
		if list, ok := strings.CutSuffix(k, "[]"); ok {
			prior, _ := fields[list].([]any)
			fields[list] = append(prior, value(v))
			return
		}
		fields[k] = value(v)
	}
	for _, kv := range c.flags["--raw-field"] {
		add(kv, func(v string) any { return v })
	}
	for _, kv := range c.flags["--field"] {
		add(kv, g.typed)
	}
	return fields
}

// typed returns the value of a -F field as gh sends it: true, false and
// null as JSON's, an integer as a number, and a string with its {owner} and
// {repo} placeholders filled.
func (g *GitHub) typed(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return g.resolve(v)
}

// runAPI answers a gh api call that checkAPI parsed.
func runAPI(g *GitHub, c *call) Reply {
	if c.api.path == "graphql" {
		return g.graphql(c)
	}
	res := c.api.route.handle(g, c)
	switch {
	case res.status >= http.StatusBadRequest:
		return apiError(res.status, res.message)
	case res.body == nil:
		return printed(res.text)
	}
	return output(res.body, c.flag("--jq"), c.inv.Env, false)
}

// apiError returns gh api's reply to an HTTP error: GitHub's error body on
// stdout and the message with the status on stderr.
func apiError(status int, message string) Reply {
	body, err := encode(object{{"message", message}, {"documentation_url", "https://docs.github.com/rest"},
		{"status", strconv.Itoa(status)}})
	if err != nil {
		body = nil
	}
	return Reply{Stdout: body, Stderr: fmt.Appendf(nil, "gh: %s (HTTP %d)\n", message, status), Code: 1}
}

// ours reports whether a's owner and repo captures name the repository.
func (g *GitHub) ours(a *apiCall) bool {
	return strings.EqualFold(a.params["owner"], g.owner) && strings.EqualFold(a.params["repo"], g.name)
}

// getUser answers GET user: the viewer.
func getUser(g *GitHub, _ *call) apiResult {
	return apiResult{status: http.StatusOK, body: g.user(g.viewer)}
}

// getRepo answers GET repos/:owner/:repo.
func getRepo(g *GitHub, c *call) apiResult {
	if !g.ours(c.api) {
		return notFound()
	}
	ownerType := typeUser
	for team := range g.teams {
		if org, _, _ := strings.Cut(team, "/"); strings.EqualFold(org, g.owner) {
			ownerType = "Organization"
		}
	}
	full := g.owner + "/" + g.name
	return apiResult{status: http.StatusOK, body: object{
		{"id", accountID(full)}, {"node_id", "R_" + strconv.FormatInt(accountID(full), 10)}, {keyName, g.name},
		{"full_name", full}, {"private", false},
		{"owner", object{{keyLogin, g.owner}, {"id", accountID(g.owner)}, {keyType, ownerType}}},
		{"html_url", htmlBase + "/" + full},
	}}
}

// getContents answers GET repos/:owner/:repo/contents/*path with the
// file's raw text.
func getContents(g *GitHub, c *call) apiResult {
	text, ok := g.files[c.api.params["path"]]
	if !ok || !g.ours(c.api) {
		return notFound()
	}
	return apiResult{status: http.StatusOK, text: text}
}

// getMembers answers GET orgs/:org/teams/:team/members.
func getMembers(g *GitHub, c *call) apiResult {
	members, ok := g.teams[c.api.params["org"]+"/"+c.api.params["team"]]
	if !ok {
		return notFound()
	}
	users := make([]any, 0, len(members))
	for _, m := range members {
		users = append(users, g.user(m))
	}
	return apiResult{status: http.StatusOK, body: users}
}

// user returns the account login as GitHub's REST API shows it.
func (g *GitHub) user(login string) object {
	kind := typeUser
	if strings.HasSuffix(login, "[bot]") {
		kind = "Bot"
	}
	return object{{keyLogin, login}, {"id", accountID(login)}, {keyType, kind}, {"site_admin", false}}
}
