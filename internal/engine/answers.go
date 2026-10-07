package engine

import (
	"context"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// readAnswers lists every comment on the issue of c's run through the
// tracker's port.CommentLister, within lookupTimeout, and posts them for
// the core to pick the answers from (KTD-W6). A listing that fails or
// times out, or a tracker that lists no comments, posts a failed read with
// its scrubbed reason, and the session still starts (R48). The comments go
// only to the core: the engine logs and reports none of them.
func (e *Engine) readAnswers(ctx context.Context, c core.ReadAnswers) {
	read := core.AnswersRead{IssueID: c.IssueID, Run: c.Run, Action: c.Action}
	lister, ok := e.cfg.Tracker.(port.CommentLister)
	if !ok {
		read.Failed, read.Reason = true, crew.NewSessionText("the tracker lists no comments")
		e.post(read)
		return
	}
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()
	comments, err := lister.Comments(ctx, c.IssueID)
	if err != nil {
		read.Failed, read.Reason = true, crew.NewSessionText(e.scrub(err.Error()))
	} else {
		read.Comments = comments
	}
	e.post(read)
}
