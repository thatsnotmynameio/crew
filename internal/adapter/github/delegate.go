package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Delegate implements port.Delegator: one new comment on the issue, posted
// as the writer, that renders delegation (renderDelegation). Its errors
// are postComment's.
func (t *Tracker) Delegate(ctx context.Context, delegation crew.Delegation) error {
	if _, _, err := t.postComment(ctx, delegation.IssueID.Key, renderDelegation(delegation)); err != nil {
		return fmt.Errorf("delegate the question on issue #%s: %w", delegation.IssueID.Key, err)
	}
	return nil
}

// renderDelegation renders d as one Markdown comment: the answerer's
// mention (mention), then the question crew asks it to answer, by its id
// and the rule that asked it, or that crew found no open question or could
// not read the comments, then the delegation's marker on a line of its own
// (KTD8, KTD9). It never quotes the question, which the issue shows.
func renderDelegation(d crew.Delegation) string {
	var line string
	switch d.Search {
	case crew.QuestionFound:
		line = fmt.Sprintf("crew asks you to answer the question %s that %s asked on %s.",
			codeSpan(string(d.ID)), codeSpan(string(d.Rule)), d.IssueRef)
	case crew.QuestionNotFound:
		line = fmt.Sprintf("crew was to ask you to answer a question on %s, "+
			"and found no open question in its comments.", d.IssueRef)
	case crew.QuestionUnread:
		line = fmt.Sprintf("crew was to ask you to answer a question on %s, and could not read its comments.", d.IssueRef)
	}
	return fmt.Sprintf("%s, %s\n\n%s\n", mention(d.Answerer), line, crew.DelegatedMarker(d.ID))
}

// mention returns how a comment mentions login: @login for a user, and
// @<slug> in a code span for an App, <slug>[bot], so GitHub notifies no
// user who owns the login <slug>, while an App that reads the comment's
// text still finds it.
func mention(login string) string {
	if slug, app := strings.CutSuffix(login, "[bot]"); app {
		return codeSpan("@" + slug)
	}
	return "@" + login
}
