package worktrees

import (
	"context"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/fake"
)

func TestCleanStoppedWhileListingReportsStoppedNotAnError(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.ws.ScriptWorkspaces(fake.Listing{Err: context.Canceled})
	o, out := r.options(strings.NewReader(""), true)

	res, err := Clean(ctx, o)
	if err != nil {
		t.Fatalf("Clean = %v, want no error: a stop is not an environment error", err)
	}

	wantResult(t, res, Stopped)
	if out.String() != "Stopped; nothing was removed.\n" {
		t.Fatalf("output: got %q", out.String())
	}
}
