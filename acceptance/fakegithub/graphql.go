package fakegithub

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// gqlNode is a GraphQL object: its type's name and the fields it resolves.
type gqlNode struct {
	typename string
	fields   map[string]gqlField
}

// gqlField is a field of a gqlNode: the arguments it takes and how it
// resolves them to a scalar, a gqlNode, a list of them, or nil.
type gqlField struct {
	args    []string
	resolve func(args map[string]any) (any, error)
}

// unknownError is a part of a query the fake does not know: a violation.
type unknownError struct {
	what string
}

func (e *unknownError) Error() string { return e.what }

// notFoundError is GitHub's NOT_FOUND error on one field, which then
// resolves to null.
type notFoundError struct {
	message string
}

func (e *notFoundError) Error() string { return e.message }

// checkGraphQL parses a gh api graphql call's query and returns what the
// fake does not know about it.
func checkGraphQL(a *apiCall, c *call) string {
	query, ok := a.fields["query"].(string)
	switch {
	case !ok:
		return "gh api graphql without a query field"
	case a.method != http.MethodPost || a.raw || c.has("--paginate"):
		return "gh api graphql with " + a.method + ", a header or --paginate"
	}
	doc, err := parser.ParseQuery(&ast.Source{Input: query})
	if err != nil {
		return "unparsable GraphQL query: " + err.Error()
	}
	if len(doc.Operations) != 1 || doc.Operations[0].Operation != ast.Query {
		return "GraphQL document that is not one query"
	}
	a.doc = doc
	return ""
}

// gqlRun is one GraphQL query being resolved.
type gqlRun struct {
	doc      *ast.QueryDocument
	vars     map[string]any
	errs     []any    // GitHub's errors, as it writes them
	messages []string // their messages, as gh prints them
}

// graphql answers gh api graphql: the query resolved against the
// repository, with GitHub's errors on stderr as gh prints them.
func (g *GitHub) graphql(c *call) Reply {
	vars := map[string]any{}
	for k, v := range c.api.fields {
		if k != "query" {
			vars[k] = v
		}
	}
	r := &gqlRun{doc: c.api.doc, vars: vars}
	data, err := r.selectNode(g.queryNode(), c.api.doc.Operations[0].SelectionSet, nil)
	if unknown, ok := errors.AsType[*unknownError](err); ok {
		return violation(c.inv.Args, unknown.what)
	}
	if err != nil {
		return failed(err.Error())
	}
	reply := object{{"data", data}}
	if len(r.errs) == 0 {
		return output(reply, c.flag("--jq"), c.inv.Env, false)
	}
	body, err := encode(reply.set("errors", r.errs))
	if err != nil {
		return failed(err.Error())
	}
	return Reply{Stdout: body, Stderr: []byte("gh: " + strings.Join(r.messages, "\n") + "\n"), Code: 1}
}

// selectNode returns the fields set selects of n.
func (r *gqlRun) selectNode(n gqlNode, set ast.SelectionSet, path []any) (object, error) {
	return r.merge(n, set, object{}, path)
}

// selectOne adds to out what sel, a field or a fragment, selects of n.
func (r *gqlRun) selectOne(n gqlNode, sel ast.Selection, out object, path []any) (object, error) {
	if f, ok := sel.(*ast.Field); ok {
		return r.selectField(n, f, out, path)
	}
	if f, ok := sel.(*ast.InlineFragment); ok {
		if f.TypeCondition != "" && f.TypeCondition != n.typename {
			return out, nil
		}
		return r.merge(n, f.SelectionSet, out, path)
	}
	if s, ok := sel.(*ast.FragmentSpread); ok {
		def := r.doc.Fragments.ForName(s.Name)
		if def == nil {
			return nil, &unknownError{"unknown GraphQL fragment " + s.Name}
		}
		if def.TypeCondition != n.typename {
			return out, nil
		}
		return r.merge(n, def.SelectionSet, out, path)
	}
	return nil, &unknownError{"unknown GraphQL selection"}
}

// merge adds to out the fields set selects of n.
func (r *gqlRun) merge(n gqlNode, set ast.SelectionSet, out object, path []any) (object, error) {
	for _, sel := range set {
		var err error
		if out, err = r.selectOne(n, sel, out, path); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// selectField adds to out the field f of n, under its alias.
func (r *gqlRun) selectField(n gqlNode, f *ast.Field, out object, path []any) (object, error) {
	key := f.Alias
	if key == "" {
		key = f.Name
	}
	if f.Name == "__typename" {
		return out.set(key, n.typename), nil
	}
	field, ok := n.fields[f.Name]
	if !ok {
		return nil, &unknownError{"unknown GraphQL field " + n.typename + "." + f.Name}
	}
	args, err := r.args(n.typename, f, field)
	if err != nil {
		return nil, err
	}
	at := append(append([]any{}, path...), key)
	v, err := field.resolve(args)
	if missing, ok := errors.AsType[*notFoundError](err); ok {
		r.errs = append(r.errs, object{{keyType, "NOT_FOUND"}, {"path", at},
			{"locations", []any{object{{"line", f.Position.Line}, {"column", f.Position.Column}}}},
			{"message", missing.message}})
		r.messages = append(r.messages, missing.message)
		return out.set(key, nil), nil
	}
	if err != nil {
		return nil, err
	}
	v, err = r.shape(v, f, at)
	if err != nil {
		return nil, err
	}
	return out.set(key, v), nil
}

// args returns the arguments of f, a field of the type typename, with the
// query's variables filled in.
func (r *gqlRun) args(typename string, f *ast.Field, field gqlField) (map[string]any, error) {
	args := map[string]any{}
	for _, a := range f.Arguments {
		if !slices.Contains(field.args, a.Name) {
			return nil, &unknownError{"unknown GraphQL argument " + typename + "." + f.Name + "(" + a.Name + ")"}
		}
		v, err := a.Value.Value(r.vars)
		if err != nil {
			return nil, &unknownError{"unreadable GraphQL argument " + a.Name}
		}
		args[a.Name] = v
	}
	return args, nil
}

// shape returns v, what f resolved to, with f's selection applied to a
// node or to each node of a list.
func (r *gqlRun) shape(v any, f *ast.Field, path []any) (any, error) {
	if n, ok := v.(gqlNode); ok {
		if len(f.SelectionSet) == 0 {
			return nil, &unknownError{"GraphQL field " + f.Name + " without a selection"}
		}
		return r.selectNode(n, f.SelectionSet, path)
	}
	if list, ok := v.([]gqlNode); ok {
		out := make([]any, 0, len(list))
		for i, n := range list {
			o, err := r.selectNode(n, f.SelectionSet, append(append([]any{}, path...), i))
			if err != nil {
				return nil, err
			}
			out = append(out, o)
		}
		return out, nil
	}
	if len(f.SelectionSet) > 0 {
		return nil, &unknownError{"GraphQL selection on the scalar " + f.Name}
	}
	return v, nil
}
