package engine

import (
	"context"

	"github.com/thatsnotmynameio/crew/internal/core"
	"github.com/thatsnotmynameio/crew/internal/crew"
)

// Repository returns the repository the engine read in Prepare, for the
// black-box tests.
func (e *Engine) Repository() crew.Repository { return e.repository }

// ReadQuestion runs the read of the comments c asks for, as the engine's
// goroutine does, and returns the input it posts, for the black-box tests.
func (e *Engine) ReadQuestion(ctx context.Context, c core.ReadQuestion) core.Input {
	e.readQuestion(ctx, c)
	return (<-e.inbox).input
}

// ReadReturn runs the read of the comments c asks for, as the engine's
// goroutine does, and returns the input it posts, for the black-box tests.
func (e *Engine) ReadReturn(ctx context.Context, c core.ReadReturn) core.Input {
	e.readReturn(ctx, c)
	return (<-e.inbox).input
}
