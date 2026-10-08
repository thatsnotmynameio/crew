package engine

import (
	"context"
	"fmt"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// comment posts a route's comment through the tracker's port.Commenter, and
// answers with its result classified as a move's is, so the core can owe
// it (KTD9). A tracker without one refuses: crew refuses a config whose
// routes comment on such a tracker before it starts.
func (e *Engine) comment(ctx context.Context, c core.Comment) {
	if e.commenter == nil {
		e.post(e.callResult(ctx, c.ID, fmt.Errorf("the tracker cannot comment: %w", port.ErrRefused)))
		return
	}
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.commenter.Comment(ctx, c.IssueID, c.Body)
	e.post(e.callResult(ctx, c.ID, err))
}

// close closes the issue for a route through the tracker's port.Closer,
// and answers as comment does.
func (e *Engine) close(ctx context.Context, c core.Close) {
	if e.closer == nil {
		e.post(e.callResult(ctx, c.ID, fmt.Errorf("the tracker cannot close issues: %w", port.ErrRefused)))
		return
	}
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.closer.Close(ctx, c.IssueID, c.From)
	e.post(e.callResult(ctx, c.ID, err))
}

// delegate posts a delegation step's delegation through the tracker's
// port.Delegator, and answers as comment does.
func (e *Engine) delegate(ctx context.Context, c core.Delegate) {
	if e.delegator == nil {
		e.post(e.callResult(ctx, c.ID, fmt.Errorf("the tracker cannot delegate: %w", port.ErrRefused)))
		return
	}
	ctx, cancel := callContext(ctx)
	defer cancel()
	err := e.delegator.Delegate(ctx, c.Delegation)
	e.post(e.callResult(ctx, c.ID, err))
}
