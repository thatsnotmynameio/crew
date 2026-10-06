package fakegithub

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// baseTime is the time every derived timestamp counts from.
func baseTime() time.Time {
	return time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
}

// now returns the time of a new write: a minute after the last one.
// The caller holds g.mu.
func (g *GitHub) now() time.Time {
	g.ticks++
	return baseTime().Add(time.Duration(g.ticks) * time.Minute)
}

// touch wakes whoever waits on Changed. The caller holds g.mu.
func (g *GitHub) touch() {
	close(g.changed)
	g.changed = make(chan struct{})
}

// ensureLabels creates the labels among names the repository lacks and
// returns names in the repository's spelling. The caller holds g.mu.
func (g *GitHub) ensureLabels(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		label, ok := g.label(n)
		if !ok {
			g.labels = append(g.labels, n)
			label = n
		}
		out = append(out, label)
	}
	return out
}

// label returns the repository's label named name, ignoring case.
func (g *GitHub) label(name string) (string, bool) {
	i := slices.IndexFunc(g.labels, func(l string) bool { return strings.EqualFold(l, name) })
	if i < 0 {
		return "", false
	}
	return g.labels[i], true
}

// add stores it under number, or the next free number when it is 0, with
// its defaults filled in. The caller holds g.mu.
func (g *GitHub) add(number int, it *item) int {
	if number == 0 {
		number = len(g.items) + 1
		for g.items[number] != nil {
			number++
		}
	}
	if g.items[number] != nil {
		panic(fmt.Sprintf("fakegithub: number %d is taken", number))
	}
	it.number = number
	if it.author == "" {
		it.author = g.viewer
	}
	if it.state == "" {
		it.state = Open
	}
	if it.createdAt.IsZero() {
		it.createdAt = baseTime().Add(time.Duration(number) * time.Hour)
	}
	it.labels = g.ensureLabels(it.labels)
	g.items[number] = it
	g.touch()
	return number
}

// comment stores a new comment and returns it. The caller holds g.mu.
func (g *GitHub) comment(number int, author, body string) *comment {
	const firstID = 1001
	at := g.now()
	c := &comment{id: int64(firstID + len(g.comments)), number: number, author: author, body: body,
		created: at, updated: at}
	g.comments = append(g.comments, c)
	g.touch()
	return c
}

// mustItem returns the issue or pull request number, and panics without
// one. The caller holds g.mu.
func (g *GitHub) mustItem(number int) *item {
	it := g.items[number]
	if it == nil {
		panic(fmt.Sprintf("fakegithub: no issue or pull request #%d", number))
	}
	return it
}

// resolve fills the {owner} and {repo} placeholders of s with the
// repository's owner and name, as gh does.
func (g *GitHub) resolve(s string) string {
	return strings.NewReplacer("{owner}", g.owner, "{repo}", g.name).Replace(s)
}
