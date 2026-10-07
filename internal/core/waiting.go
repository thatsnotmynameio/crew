package core

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// checkLimit is the longest one check of a waiting session may take: it
// waits through repeated checks, never one command as long as its wait
// (KTD-W10).
const checkLimit = 5 * time.Minute

// WithAnswerers gives the model who may answer a question a session asks
// on its issue: the code owners the tracker found and the answering list
// (R37, R38, KTD-W4). Without it, no one may.
func WithAnswerers(a crew.Answerers) Option {
	return func(m *Model) {
		m.answerers = crew.Answerers{CodeOwners: slices.Clone(a.CodeOwners), Apps: slices.Clone(a.Apps)}
	}
}

// waiting is what the waiting paragraph tells a session that may wait
// (KTD-W10).
type waiting struct {
	// issue is the issue's number.
	issue string
	// wait is how long the session waits for an answer.
	wait time.Duration
	// marker is the session's marker.
	marker string
	// login is the login the session acts as; empty when unknown.
	login string
	// owners are the code owners' logins.
	owners []string
	// apps are the answering list's logins, without the session's own.
	apps []string
}

// waitingOf returns what the waiting paragraph tells the session of h's
// action named name, whose spec is spec: the answering list without the
// login it acts as, compared ignoring case (R40, KTD-W8).
func (m *Model) waitingOf(h *heldRun, name crew.ActionName, spec crew.SessionSpec) waiting {
	login := m.bots.login(spec.Bot.Name)
	apps := slices.DeleteFunc(slices.Clone(m.answerers.Apps), func(app string) bool {
		return strings.EqualFold(app, login)
	})
	return waiting{
		issue: h.run.Issue().ID().Key, wait: spec.Wait, marker: crew.SessionMarker(h.run.ID(), name), login: login,
		owners: m.answerers.CodeOwners, apps: apps,
	}
}

// readCommand returns the one command a waiting session reads the issue's
// comments with: every page of them, each filtered on its own by
// answerFilter, so no other comment's body reaches the session (KTD-W10).
func readCommand(w waiting) string {
	return "gh api --paginate " + shellQuote("repos/{owner}/{repo}/issues/"+w.issue+"/comments?per_page=100") +
		" --jq " + shellQuote(answerFilter(w))
}

// answerFilter returns the jq program that turns one page of comments into
// one JSON object per line: each comment by the session's login that holds
// its marker and not crew's own, as its question, with when it was
// written, and each comment that holds none of crew's markers by a code
// owner who is not an App or by an App on the list, with when and by whom
// it was written and its body. Logins compare in lower case. With the
// session's login unknown, it prints no question.
func answerFilter(w waiting) string {
	answer := "($b | contains(" + jqString(crew.MarkerPrefix) + ") | not) and " +
		"((.user.type != \"Bot\" and any(" + jqList(w.owners) + "[]; . == $l)) or " +
		"(.user.type == \"Bot\" and any(" + jqList(w.apps) + "[]; . == $l)))"
	pick := "if " + answer + " then {created_at, login: .user.login, body: $b} else empty end"
	if w.login != "" {
		question := "$l == " + jqString(asciiLower(w.login)) +
			" and ($b | contains(" + jqString(w.marker) + "))" +
			" and ($b | contains(" + jqString(crew.PostedMarker) + ") | not)"
		pick = "if " + question + " then {question: true, created_at} elif " + strings.TrimPrefix(pick, "if ")
	}
	return `.[] | ((.user.login // "") | ascii_downcase) as $l | (.body // "") as $b | ` + pick + " | tojson"
}

// jqList returns logins in lower case as a jq array literal, without
// the empty ones.
func jqList(logins []string) string {
	items := make([]string, 0, len(logins))
	for _, l := range logins {
		if l != "" {
			items = append(items, jqString(asciiLower(l)))
		}
	}
	return "[" + strings.Join(items, ", ") + "]"
}

// jqString returns s as a jq string literal, with its quotes, backslashes
// and control characters escaped as JSON escapes them, so no value can end
// it or interpolate.
func jqString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// asciiLower returns s with its ASCII letters in lower case, as jq's
// ascii_downcase does.
func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return r
	}, s)
}

// shellQuote returns s as one word of a POSIX shell command line.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// forPeople returns d as people write it, such as "1 hour 30 minutes", or
// as Go writes it when it is not a whole number of seconds.
func forPeople(d time.Duration) string {
	if d <= 0 || d%time.Second != 0 {
		return d.String()
	}
	var parts []string
	for _, u := range []struct {
		size time.Duration
		name string
	}{{time.Hour, "hour"}, {time.Minute, "minute"}, {time.Second, "second"}} {
		if n := d / u.size; n > 0 {
			parts = append(parts, plural(int(n), u.name))
			d %= u.size
		}
	}
	return strings.Join(parts, " ")
}

// plural returns n and name, with an s unless n is 1.
func plural(n int, name string) string {
	if n == 1 {
		return "1 " + name
	}
	return strconv.Itoa(n) + " " + name + "s"
}
