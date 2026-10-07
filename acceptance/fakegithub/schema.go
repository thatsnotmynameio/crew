package fakegithub

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// scalar returns a field that resolves to v.
func scalar(v any) gqlField {
	return gqlField{resolve: func(map[string]any) (any, error) { return v, nil }}
}

// queryNode returns the query root.
func (g *GitHub) queryNode() gqlNode {
	return gqlNode{typename: "Query", fields: map[string]gqlField{
		"repository": {args: []string{"owner", keyName}, resolve: func(a map[string]any) (any, error) {
			owner, _ := a["owner"].(string)
			name, _ := a[keyName].(string)
			if !strings.EqualFold(owner, g.owner) || !strings.EqualFold(name, g.name) {
				return nil, &notFoundError{fmt.Sprintf("Could not resolve to a Repository with the name '%s/%s'.",
					owner, name)}
			}
			return g.repositoryNode(), nil
		}},
	}}
}

// repositoryNode returns the repository.
func (g *GitHub) repositoryNode() gqlNode {
	return gqlNode{typename: "Repository", fields: map[string]gqlField{
		"id":                 scalar(g.repositoryID()),
		"nameWithOwner":      scalar(g.owner + "/" + g.name),
		"issues":             {args: []string{keyFirst, "states", "filterBy", "orderBy", keyLabels}, resolve: g.issues},
		"pullRequests":       {args: []string{keyFirst, "states", keyLabels, "orderBy", keyHead}, resolve: g.pulls},
		"issueOrPullRequest": {args: []string{keyNumber}, resolve: g.issueOrPullRequest},
	}}
}

// repositoryID returns the node id GitHub gives the repository, the same
// in GraphQL's id and in the REST API's node_id.
func (g *GitHub) repositoryID() string {
	return fmt.Sprintf("R_%d", accountID(g.owner+"/"+g.name))
}

// issues resolves Repository.issues: the issues in the states, with any of
// the labels, by the createdBy author, ordered and cut to the first ones.
func (g *GitHub) issues(args map[string]any) (any, error) {
	filter, _ := args["filterBy"].(map[string]any)
	for k := range filter {
		if k != "createdBy" && k != keyLabels {
			return nil, &unknownError{"unknown GraphQL issues filter " + k}
		}
	}
	author, _ := filter["createdBy"].(string)
	labels := slices.Concat(strs(args[keyLabels]), strs(filter[keyLabels]))
	return g.connection("IssueConnection", args, func(it *item) bool {
		return it.pull == nil && (author == "" || strings.EqualFold(it.author, author)) && hasAny(it, labels)
	})
}

// pulls resolves Repository.pullRequests: the pull requests in the
// states, with any of the labels, from the headRefName branch, ordered and
// cut to the first ones.
func (g *GitHub) pulls(args map[string]any) (any, error) {
	head, _ := args[keyHead].(string)
	labels := strs(args[keyLabels])
	return g.connection("PullRequestConnection", args, func(it *item) bool {
		return it.pull != nil && (head == "" || it.pull.head == head) && hasAny(it, labels)
	})
}

// connection returns the items keep keeps that are in the states args
// names, in the orderBy order, cut to the first ones, as a connection of
// type typename.
func (g *GitHub) connection(typename string, args map[string]any, keep func(*item) bool) (any, error) {
	states := strs(args["states"])
	var list []*item
	for _, it := range g.items {
		if keep(it) && (len(states) == 0 || slices.Contains(states, string(it.state))) {
			list = append(list, it)
		}
	}
	order, _ := args["orderBy"].(map[string]any)
	if field, ok := order["field"]; ok && field != "CREATED_AT" {
		return nil, &unknownError{fmt.Sprintf("unknown GraphQL order %v", field)}
	}
	slices.SortFunc(list, func(a, b *item) int {
		return cmp.Or(a.createdAt.Compare(b.createdAt), cmp.Compare(a.number, b.number))
	})
	if order["direction"] == "DESC" {
		slices.Reverse(list)
	}
	nodes := make([]gqlNode, 0, len(list))
	for _, it := range first(list, args) {
		nodes = append(nodes, g.itemNode(it))
	}
	return connectionOf(typename, nodes), nil
}

// connectionOf returns a connection of type typename holding nodes.
func connectionOf(typename string, nodes []gqlNode) gqlNode {
	return gqlNode{typename: typename, fields: map[string]gqlField{"nodes": scalar(nodes)}}
}

// first returns list cut to the first argument of args, when it has one.
func first[T any](list []T, args map[string]any) []T {
	n, ok := toInt(args[keyFirst])
	if ok && n >= 0 && n < len(list) {
		return list[:n]
	}
	return list
}

// toInt returns v, a GraphQL Int from the query or from a variable, as an
// int.
func toInt(v any) (int, bool) {
	if n, ok := v.(int); ok {
		return n, true
	}
	if n, ok := v.(int64); ok {
		return int(n), true
	}
	return 0, false
}

// strs returns v, a GraphQL list of strings or one string, as strings.
func strs(v any) []string {
	if s, ok := v.(string); ok {
		return []string{s}
	}
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// hasAny reports whether it carries any of labels, ignoring case, or
// labels is empty.
func hasAny(it *item, labels []string) bool {
	if len(labels) == 0 {
		return true
	}
	return slices.ContainsFunc(it.labels, func(l string) bool {
		return slices.ContainsFunc(labels, func(want string) bool { return strings.EqualFold(l, want) })
	})
}

// issueOrPullRequest resolves Repository.issueOrPullRequest.
func (g *GitHub) issueOrPullRequest(args map[string]any) (any, error) {
	n, _ := toInt(args[keyNumber])
	it := g.items[n]
	if it == nil {
		return nil, &notFoundError{fmt.Sprintf("Could not resolve to an issue or pull request with the number of %d.", n)}
	}
	return g.itemNode(it), nil
}

// itemNode returns it as an Issue or a PullRequest.
func (g *GitHub) itemNode(it *item) gqlNode {
	fields := map[string]gqlField{
		keyNumber:    scalar(it.number),
		keyTitle:     scalar(it.title),
		keyURL:       scalar(g.htmlURL(it.number)),
		keyCreatedAt: scalar(it.createdAt.Format(time.RFC3339)),
		keyState:     scalar(string(it.state)),
		"repository": scalar(g.repositoryNode()),
		keyLabels: {args: []string{keyFirst}, resolve: func(a map[string]any) (any, error) {
			return labelsNode(it, a), nil
		}},
	}
	if it.pull != nil {
		fields["author"] = scalar(authorNode(it.author))
		fields[keyHead] = scalar(it.pull.head)
		fields["isCrossRepository"] = scalar(it.pull.cross)
		return gqlNode{typename: "PullRequest", fields: fields}
	}
	fields["issueDependenciesSummary"] = scalar(gqlNode{typename: "IssueDependenciesSummary",
		fields: map[string]gqlField{"blockedBy": scalar(g.openBlockers(it))}})
	fields["issueFieldValues"] = gqlField{args: []string{keyFirst}, resolve: func(a map[string]any) (any, error) {
		return connectionOf("IssueFieldValueConnection", first(g.fieldValues(it), a)), nil
	}}
	fields["closedByPullRequestsReferences"] = gqlField{args: []string{keyFirst, "includeClosedPrs"},
		resolve: func(a map[string]any) (any, error) { return g.closing(it, a), nil }}
	return gqlNode{typename: "Issue", fields: fields}
}

// labelsNode returns the labels of it as a LabelConnection.
func labelsNode(it *item, args map[string]any) gqlNode {
	nodes := make([]gqlNode, 0, len(it.labels))
	for _, l := range first(it.labels, args) {
		nodes = append(nodes, gqlNode{typename: "Label", fields: map[string]gqlField{keyName: scalar(l)}})
	}
	return connectionOf("LabelConnection", nodes)
}

// authorNode returns the account login as GraphQL shows an author: a
// GitHub App's as a Bot, without its "[bot]".
func authorNode(login string) gqlNode {
	if app, ok := strings.CutSuffix(login, "[bot]"); ok {
		return gqlNode{typename: "Bot", fields: map[string]gqlField{keyLogin: scalar(app)}}
	}
	return gqlNode{typename: typeUser, fields: map[string]gqlField{keyLogin: scalar(login)}}
}

// openBlockers returns how many open issues block it.
func (g *GitHub) openBlockers(it *item) int {
	n := 0
	for _, b := range it.blockedBy {
		if blocker := g.items[b]; blocker != nil && blocker.state == Open {
			n++
		}
	}
	return n
}

// fieldValues returns its issue field values: its Priority option, if any.
func (g *GitHub) fieldValues(it *item) []gqlNode {
	if it.priority == "" {
		return []gqlNode{}
	}
	options := make([]gqlNode, 0, len(g.priorities))
	for _, o := range g.priorities {
		options = append(options, gqlNode{typename: "IssueFieldSingleSelectOption",
			fields: map[string]gqlField{"id": scalar(optionID(o)), keyName: scalar(o)}})
	}
	field := gqlNode{typename: "IssueFieldSingleSelect",
		fields: map[string]gqlField{keyName: scalar("Priority"), "options": scalar(options)}}
	return []gqlNode{{typename: "IssueFieldSingleSelectValue", fields: map[string]gqlField{
		"optionId": scalar(optionID(it.priority)), keyName: scalar(it.priority), "field": scalar(field)}}}
}

// optionID returns the node id GitHub gives the Priority option name.
func optionID(name string) string {
	return fmt.Sprintf("IFSSO_%d", accountID("priority:"+name))
}

// closing returns the pull requests that close it, as GitHub links them:
// open and merged ones, and closed ones too with includeClosedPrs, in
// number order.
func (g *GitHub) closing(it *item, args map[string]any) gqlNode {
	withClosed, _ := args["includeClosedPrs"].(bool)
	var prs []*item
	for _, pr := range g.items {
		if pr.pull != nil && slices.Contains(pr.pull.closes, it.number) && (withClosed || pr.state != Closed) {
			prs = append(prs, pr)
		}
	}
	slices.SortFunc(prs, func(a, b *item) int { return cmp.Compare(a.number, b.number) })
	nodes := make([]gqlNode, 0, len(prs))
	for _, pr := range first(prs, args) {
		nodes = append(nodes, g.itemNode(pr))
	}
	return connectionOf("PullRequestConnection", nodes)
}
