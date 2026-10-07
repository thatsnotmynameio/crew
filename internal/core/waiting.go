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
	// earlier are the open questions earlier sessions at the action asked,
	// whose answers the session may still wait for (KTD-W7).
	earlier []asked
	// owners are the code owners' logins.
	owners []string
	// apps are the answering list's logins, without the session's own and
	// those that asked the earlier questions.
	apps []string
}

// waitingOf returns what the waiting paragraph tells the session of h's
// action named name, whose spec is spec: the open questions earlier
// sessions at the action asked, and the answering list without the login
// it acts as and theirs, compared ignoring case (R40, KTD-W7, KTD-W8).
func (m *Model) waitingOf(h *heldRun, name crew.ActionName, spec crew.SessionSpec) waiting {
	login := m.bots.login(spec.Bot.Name)
	earlier, asking := askedAt(h.run.Questions(name))
	return waiting{
		issue: h.run.Issue().ID().Key, wait: spec.Wait, marker: crew.SessionMarker(h.run.ID(), name), login: login,
		earlier: earlier, owners: m.answerers.CodeOwners, apps: m.appsExcept(append(asking, login)...),
	}
}

// askedAt returns what a read command prints of questions, each by its
// marker and login, and the logins that asked them.
func askedAt(questions []crew.Question) ([]asked, []string) {
	asks := make([]asked, 0, len(questions))
	logins := make([]string, 0, len(questions))
	for _, q := range questions {
		asks = append(asks, asked{marker: crew.SessionMarker(q.Run, q.Action), login: q.Login})
		logins = append(logins, q.Login)
	}
	return asks, logins
}

// appsExcept returns the answering list without the logins that asked,
// compared ignoring case: an App never answers its own question (R37).
func (m *Model) appsExcept(asking ...string) []string {
	return slices.DeleteFunc(slices.Clone(m.answerers.Apps), func(app string) bool {
		return slices.ContainsFunc(asking, func(login string) bool { return strings.EqualFold(login, app) })
	})
}

// reader is what a read command reads on an issue: the questions it
// prints, by their markers and the logins they were asked as, and who may
// answer them.
type reader struct {
	// issue is the issue's number.
	issue string
	// questions are the questions' markers and logins; one with an empty
	// login prints nothing.
	questions []asked
	// owners are the code owners' logins.
	owners []string
	// apps are the answering list's logins, without those of the questions.
	apps []string
}

// asked is a question a read command prints: the marker of the session
// that asked it and the login it acted as.
type asked struct {
	marker string
	login  string
}

// reader returns what the read command of w reads: the earlier questions
// at its action and its session's own, and who may answer them.
func (w waiting) reader() reader {
	questions := append(slices.Clone(w.earlier), asked{marker: w.marker, login: w.login})
	return reader{issue: w.issue, questions: questions, owners: w.owners, apps: w.apps}
}

// readCommand returns the one command a session reads the issue's
// comments with: every page of them, each filtered on its own by
// answerFilter, so no other comment's body reaches the session (KTD-W10).
func readCommand(r reader) string {
	return "gh api --paginate " + shellQuote("repos/{owner}/{repo}/issues/"+r.issue+"/comments?per_page=100") +
		" --jq " + shellQuote(answerFilter(r))
}

// answerFilter returns the jq program that turns one page of comments into
// one JSON object per line: each comment by the login of one of r's
// questions that holds that question's marker and not crew's own, as a
// question, with when it was written, and each comment that holds none of
// crew's markers by a code owner who is not an App or by an App on the
// list, with when and by whom it was written and its body. Logins compare
// in lower case. A question whose login is unknown prints nothing.
func answerFilter(r reader) string {
	answer := "($b | contains(" + jqString(crew.MarkerPrefix) + ") | not) and " +
		"((.user.type != \"Bot\" and any(" + jqList(r.owners) + "[]; . == $l)) or " +
		"(.user.type == \"Bot\" and any(" + jqList(r.apps) + "[]; . == $l)))"
	pick := "if " + answer + " then {created_at, login: .user.login, body: $b} else empty end"
	var asks []string
	for _, q := range r.questions {
		if q.login != "" {
			asks = append(asks, "($l == "+jqString(asciiLower(q.login))+" and ($b | contains("+jqString(q.marker)+")))")
		}
	}
	if len(asks) > 0 {
		question := "(" + strings.Join(asks, " or ") + ") and ($b | contains(" + jqString(crew.PostedMarker) + ") | not)"
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
