package bots

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// The bots an older crew saved under crew/mates act without being created
// again, and every message naming their file names that one (KTD9).

func TestActMakesABotSavedByAnOlderCrewAct(t *testing.T) {
	r := newActRun(t, "ops")
	saveOpsIn(t, r.old)
	a := r.mustAct(t)
	if len(a.Bots) != 1 || a.Bots[0].Login != opsLogin || len(a.Warnings) != 0 {
		t.Errorf("Act = %+v, %q; want ops acting from crew/mates with no warning", a.Bots, a.Warnings)
	}
}

func TestTheWarningOfABotSavedByAnOlderCrewNamesItsOldFile(t *testing.T) {
	for _, tt := range []struct {
		name          string
		file          func(t *testing.T, r *actRun)
		saved         bool
		installStatus int
		want, reason  string
	}{
		{name: "unreadable file", reason: "bad key file", file: func(t *testing.T, r *actRun) {
			t.Helper()
			writeBotFile(t, r.old.Path(testOwner, "ops"), "{")
		}, want: "bot ops cannot act: the bot file <path> is not valid JSON; " +
			"delete its file and run `crew bots create ops` in this repository"},
		{name: "invalid slug", reason: "bad key file", file: func(t *testing.T, r *actRun) {
			t.Helper()
			saveOpsWithSlug("Crew Ops")(t, &actRun{store: r.old})
		}, want: "bot ops cannot act: its file <path> holds an invalid app slug; " +
			"delete it and run `crew bots create ops` in this repository"},
		{name: "key rejected", saved: true, file: func(t *testing.T, r *actRun) {
			t.Helper()
			saveOpsIn(t, r.old)
		}, installStatus: http.StatusUnauthorized, reason: "key rejected",
			want: "GitHub rejected the key of bot ops; delete <path> and run `crew bots create ops` in this repository"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newActRun(t, "ops")
			tt.file(t, r)
			r.api.installStatus = tt.installStatus
			checkUnusable(t, r.mustAct(t), tt.want, tt.reason, r.old.Path(testOwner, "ops"), tt.saved)
		})
	}
}

func TestARejectedRenewalOfABotSavedByAnOlderCrewNamesItsOldFile(t *testing.T) {
	r := newActRun(t, "ops")
	saveOpsIn(t, r.old)
	reject := false
	r.opts.mint = func(context.Context, Bot, int64, string) (Grant, error) {
		if reject {
			return Grant{}, fmt.Errorf("%w: GitHub answered 401", ErrKeyRejected)
		}
		return Grant{Token: "ghs_1", ExpiresAt: time.Now().Add(time.Hour), Permissions: permissions()}, nil
	}
	a := r.mustAct(t)
	reject = true
	if err := a.Renew(context.Background(), "ops"); err == nil {
		t.Fatal("Renew with a rejected key = nil error, want one")
	}
	checkFailing(t, a, map[string]string{"ops": "GitHub rejected the key of bot ops; delete " +
		r.old.Path(testOwner, "ops") + " and run `crew bots create ops` in this repository"})
}
