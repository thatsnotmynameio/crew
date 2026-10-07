package registry

import (
	"github.com/thatsnotmynameio/crew/internal/adapter/claude"
	"github.com/thatsnotmynameio/crew/internal/adapter/codex"
	"github.com/thatsnotmynameio/crew/internal/adapter/github"
	"github.com/thatsnotmynameio/crew/internal/port"
	"github.com/thatsnotmynameio/crew/internal/proc"
)

// Default returns the production registry: every adapter compiled into
// crew, each starting its processes through group, so a forced exit can kill
// them all. A new adapter, or a new function, is one more line here.
func Default(group *proc.Group) Registry {
	return New(
		map[string]port.TrackerFactory{
			"github": github.Factory(group),
		},
		map[string]port.HarnessFactory{
			"claude": claude.Factory(group),
			"codex":  codex.Factory(group),
		},
		nil, // crew has no function yet
	)
}
