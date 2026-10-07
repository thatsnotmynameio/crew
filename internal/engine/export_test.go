package engine

import "github.com/thatsnotmynameio/crew/internal/crew"

// Repository returns the repository the engine read in Prepare, for the
// black-box tests.
func (e *Engine) Repository() crew.Repository { return e.repository }
