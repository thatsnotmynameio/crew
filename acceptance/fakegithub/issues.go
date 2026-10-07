package fakegithub

import (
	"cmp"
	"encoding/csv"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// graphqlURL is the address gh names in the HTTP errors of the commands
// that call GitHub's GraphQL API.
const graphqlURL = "https://api.github.com/graphql"

// commands returns the gh subcommands the fake knows, by their words.
func commands() map[string]command {
	return map[string]command{
		"auth status": {flags: spec(), url: apiBase + "/", check: noArgs, run: authStatus},
		"api":         apiCommand(),
		"label list": {flags: spec("--limit= -L", "--json="), url: graphqlURL,
			check: checkLabelList, run: labelList},
		"label create": {flags: spec(), url: apiBase + "/repos/{owner}/{repo}/labels",
			check: oneArg, run: labelCreate},
		"issue view": {flags: spec("--json=", "--jq= -q"), url: graphqlURL,
			check: checkIssueView, run: issueView},
		"issue edit": {flags: spec("--add-label=", "--remove-label="), url: graphqlURL,
			check: oneNumber, run: editLabels(false)},
		"pr edit": {flags: spec("--add-label=", "--remove-label="), url: graphqlURL,
			check: oneNumber, run: editLabels(true)},
		"pr list": {flags: spec("--head= -H", "--state= -s", "--limit= -L", "--json=", "--jq= -q"), url: graphqlURL,
			check: checkPRList, run: prList},
	}
}

// noArgs refuses positional arguments.
func noArgs(_ *GitHub, c *call) string {
	if len(c.args) > 0 {
		return "unexpected argument " + c.args[0]
	}
	return ""
}

// oneArg requires exactly one positional argument.
func oneArg(_ *GitHub, c *call) string {
	if len(c.args) != 1 {
		return "want one argument"
	}
	return ""
}

// oneNumber requires one positional argument, an issue or pull request
// number.
func oneNumber(_ *GitHub, c *call) string {
	if len(c.args) != 1 {
		return "want one number"
	}
	if _, err := strconv.Atoi(c.args[0]); err != nil {
		return "want a number, not " + c.args[0]
	}
	return ""
}

// checkLabelList refuses gh label list without --json, whose text the fake
// does not print, and fields it does not know.
func checkLabelList(g *GitHub, c *call) string {
	if reason := noArgs(g, c); reason != "" {
		return reason
	}
	if !c.has("--json") {
		return "gh label list without --json"
	}
	return unknownField(c.flag("--json"), []string{keyName})
}

// authStatus answers gh auth status: logged in as the viewer.
func authStatus(g *GitHub, _ *call) Reply {
	return printed("github.com\n  ✓ Logged in to github.com account " + g.viewer + " (keyring)\n")
}

// labelList answers gh label list --json: the labels, oldest first.
func labelList(g *GitHub, c *call) Reply {
	limit := len(g.labels)
	if l, err := strconv.Atoi(c.flag("--limit")); err == nil && l < limit {
		limit = l
	}
	list := make([]any, 0, limit)
	for _, l := range g.labels[:limit] {
		list = append(list, map[string]any{keyName: l})
	}
	return output(list, "", nil, true)
}

// labelCreate answers gh label create: a new label, refused when one of
// that name exists, ignoring case.
func labelCreate(g *GitHub, c *call) Reply {
	name := c.args[0]
	if _, ok := g.label(name); ok {
		return failed(fmt.Sprintf("label with name %q already exists; use `--force` to update its color and description",
			name))
	}
	g.labels = append(g.labels, name)
	g.touch()
	return Reply{}
}

// issueJSONFields are the --json fields gh issue view prints.
func issueJSONFields() []string {
	return []string{keyLabels, keyNumber, keyState, keyTitle, keyURL}
}

// checkIssueView refuses gh issue view without --json, whose text the fake
// does not print, and fields it does not know.
func checkIssueView(g *GitHub, c *call) string {
	if reason := oneNumber(g, c); reason != "" {
		return reason
	}
	if !c.has("--json") {
		return "gh issue view without --json"
	}
	return unknownField(c.flag("--json"), issueJSONFields())
}

// unknownField returns what the fake does not know among the --json
// fields, "" when it knows them all.
func unknownField(fields string, known []string) string {
	for f := range strings.SplitSeq(fields, ",") {
		if !slices.Contains(known, f) {
			return "unknown JSON field " + f
		}
	}
	return ""
}

// issueView answers gh issue view --json, for an issue or a pull request.
func issueView(g *GitHub, c *call) Reply {
	n, _ := strconv.Atoi(c.args[0])
	it := g.items[n]
	if it == nil {
		return failed(fmt.Sprintf("GraphQL: Could not resolve to an issue or pull request with the number of %d. "+
			"(repository.issue)", n))
	}
	labels := make([]any, 0, len(it.labels))
	for _, l := range it.labels {
		labels = append(labels, object{{"id", labelID(l)}, {keyName, l}, {"description", ""}, {"color", "ededed"}})
	}
	all := map[string]any{keyLabels: labels, keyNumber: it.number, keyState: it.state, keyTitle: it.title,
		keyURL: g.htmlURL(it.number)}
	return output(pick(all, c.flag("--json")), c.flag("--jq"), c.inv.Env, true)
}

// patchIssue answers PATCH repos/:owner/:repo/issues/:number with
// state=closed: it closes the issue or pull request, which stays closed or
// merged when it already is. Any other state is GitHub's validation failure.
func patchIssue(g *GitHub, c *call) apiResult {
	it := g.target(c.api)
	if it == nil {
		return notFound()
	}
	if state, _ := c.api.fields[keyState].(string); state != closedState {
		return apiResult{status: http.StatusUnprocessableEntity, message: "Validation Failed"}
	}
	if it.state == Open {
		it.state = Closed
		g.touch()
	}
	number := strconv.Itoa(it.number)
	return apiResult{status: http.StatusOK, body: object{
		{keyURL, apiBase + "/repos/" + g.owner + "/" + g.name + "/issues/" + number},
		{keyHTMLURL, g.htmlURL(it.number)},
		{keyNumber, it.number},
		{keyState, closedState},
		{keyTitle, it.title},
	}}
}

// pick returns the fields of all that the --json flag names, keyed as gh
// exports them.
func pick(all map[string]any, fields string) map[string]any {
	out := map[string]any{}
	for f := range strings.SplitSeq(fields, ",") {
		out[f] = all[f]
	}
	return out
}

// labelID returns the node id GitHub gives the label name.
func labelID(name string) string {
	return "LA_" + strconv.FormatInt(accountID("label:"+name), 10)
}

// editLabels returns the handler of gh issue edit, or of gh pr edit when
// pr is set: it removes the --remove-label labels and adds the
// --add-label ones, refusing a label the repository lacks.
func editLabels(pr bool) func(g *GitHub, c *call) Reply {
	return func(g *GitHub, c *call) Reply {
		n, _ := strconv.Atoi(c.args[0])
		it := g.items[n]
		if pr && (it == nil || it.pull == nil) {
			return failed(fmt.Sprintf("GraphQL: Could not resolve to a PullRequest with the number of %d. "+
				"(repository.pullRequest)", n))
		}
		if it == nil {
			return failed(fmt.Sprintf("GraphQL: Could not resolve to an issue or pull request with the number of %d. "+
				"(repository.issue)", n))
		}
		add, remove := labelValues(c.flags["--add-label"]), labelValues(c.flags["--remove-label"])
		if l, ok := g.missing(add); ok {
			return failed(fmt.Sprintf("could not add label: '%s' not found", l))
		}
		if l, ok := g.missing(remove); ok {
			return failed(fmt.Sprintf("'%s' not found", l))
		}
		g.relabel(it, add, remove)
		return printed(g.htmlURL(n) + "\n")
	}
}

// missing returns the first of labels the repository lacks, and whether
// there is one.
func (g *GitHub) missing(labels []string) (string, bool) {
	for _, l := range labels {
		if _, ok := g.label(l); !ok {
			return l, true
		}
	}
	return "", false
}

// relabel takes the labels remove names off it, ignoring case, then puts
// those add names on it that it lacks, in the repository's spelling.
func (g *GitHub) relabel(it *item, add, remove []string) {
	it.labels = slices.DeleteFunc(it.labels, func(have string) bool {
		return slices.ContainsFunc(remove, func(r string) bool { return strings.EqualFold(r, have) })
	})
	for _, l := range add {
		if name, _ := g.label(l); !slices.Contains(it.labels, name) {
			it.labels = append(it.labels, name)
		}
	}
	g.touch()
}

// labelValues returns the labels of label flags' values, each read as
// comma-separated values, CSV-quoted, as gh reads them.
func labelValues(values []string) []string {
	var labels []string
	for _, v := range values {
		record, err := csv.NewReader(strings.NewReader(v)).Read()
		if err != nil {
			record = []string{v}
		}
		for _, l := range record {
			if l = strings.TrimSpace(l); l != "" {
				labels = append(labels, l)
			}
		}
	}
	return labels
}

// prFields are the --json fields gh pr list prints.
func prFields() []string {
	return []string{keyCreatedAt, keyHead, "isCrossRepository", keyNumber, keyState, keyTitle, keyURL}
}

// checkPRList refuses gh pr list without --json, states gh does not take
// and fields the fake does not know.
func checkPRList(g *GitHub, c *call) string {
	if reason := noArgs(g, c); reason != "" {
		return reason
	}
	if s := c.flag("--state"); s != "" && !slices.Contains([]string{"open", closedState, "merged", "all"}, s) {
		return "unknown state " + s
	}
	if !c.has("--json") {
		return "gh pr list without --json"
	}
	return unknownField(c.flag("--json"), prFields())
}

// prList answers gh pr list --json: the pull requests from the --head
// branch in the --state, newest first.
func prList(g *GitHub, c *call) Reply {
	var prs []*item
	for _, it := range g.items {
		if it.pull != nil && (!c.has("--head") || it.pull.head == c.flag("--head")) && inState(it, c.flag("--state")) {
			prs = append(prs, it)
		}
	}
	slices.SortFunc(prs, func(a, b *item) int {
		return cmp.Or(b.createdAt.Compare(a.createdAt), cmp.Compare(b.number, a.number))
	})
	if l, err := strconv.Atoi(c.flag("--limit")); err == nil && l < len(prs) {
		prs = prs[:l]
	}
	list := make([]any, 0, len(prs))
	for _, it := range prs {
		all := map[string]any{keyCreatedAt: it.createdAt.Format(time.RFC3339), keyHead: it.pull.head,
			"isCrossRepository": it.pull.cross, keyNumber: it.number, keyState: it.state, keyTitle: it.title,
			keyURL: g.htmlURL(it.number)}
		list = append(list, pick(all, c.flag("--json")))
	}
	return output(list, c.flag("--jq"), c.inv.Env, true)
}

// inState reports whether the pull request it is in gh pr list's --state:
// open by default, closed counting merged ones.
func inState(it *item, state string) bool {
	switch state {
	case "all":
		return true
	case closedState:
		return it.state != Open
	case "merged":
		return it.state == Merged
	default:
		return it.state == Open
	}
}
