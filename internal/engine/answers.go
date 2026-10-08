package engine

import (
	"context"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// readAnswers lists the comments on the issue of c's run (listComments) and
// posts them for the core to pick the answers from (KTD-W6). A read that
// fails posts its reason, and the session still starts (R48). The comments
// go only to the core: the engine logs and reports none of them.
func (e *Engine) readAnswers(ctx context.Context, c core.ReadAnswers) {
	read := core.AnswersRead{IssueID: c.IssueID, Run: c.Run, Action: c.Action}
	comments, reason, ok := e.listComments(ctx, c.IssueID)
	if !ok {
		read.Failed, read.Reason = true, crew.NewSessionText(reason)
	}
	read.Comments = comments
	e.post(read)
}

// readQuestion lists the comments on the issue of c's run (listComments)
// and posts them for the core to find the open question in, which c's
// delegation step delegates (KTD8). A read that fails is posted as failed,
// and the step still delegates. The comments go only to the core: the engine
// logs and reports none of them.
func (e *Engine) readQuestion(ctx context.Context, c core.ReadQuestion) {
	read := core.QuestionRead{IssueID: c.IssueID, Run: c.Run, Step: c.Step}
	comments, _, ok := e.listComments(ctx, c.IssueID)
	read.Comments, read.Failed = comments, !ok
	e.post(read)
}

// listComments lists every comment on the issue identified by id through
// the tracker's port.CommentLister, within lookupTimeout, and reports
// whether it could. A listing that fails or times out, or a tracker that
// lists no comments, returns its scrubbed reason instead.
func (e *Engine) listComments(ctx context.Context, id crew.IssueID) ([]crew.Comment, string, bool) {
	lister, ok := e.cfg.Tracker.(port.CommentLister)
	if !ok {
		return nil, "the tracker lists no comments", false
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	comments, err := lister.Comments(ctx, id)
	if err != nil {
		return nil, e.scrub(err.Error()), false
	}
	return comments, "", true
}
