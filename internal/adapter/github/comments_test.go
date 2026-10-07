package github

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/thatsnotmynameio/crew/internal/crew"
	"github.com/thatsnotmynameio/crew/internal/port"
)

// listedComment is one issue comment as the REST API lists it, with its
// author's type and its creation time.
func listedComment(id int, login, kind, body, created string) string {
	return fmt.Sprintf(`{"id":%d,"user":{"login":%q,"type":%q},"body":%q,"created_at":%q}`,
		id, login, kind, body, created)
}

func TestCommentsListsEveryPageOldestFirstFlaggingApps(t *testing.T) {
	page1 := "[" + strings.Join([]string{
		listedComment(7, "ana", "User", "First.", "2026-10-01T09:00:00Z"),
		listedComment(9, "crew-ops[bot]", "Bot", "crew: done.\n", "2026-10-01T10:00:00Z"),
	}, ",") + "]"
	page2 := "[" + listedComment(12, "ana", "User", "Thanks!", "2026-10-02T08:30:00Z") + "]"
	tr, gh := build(t, reply{prefix: listComments, as: asYou, stdout: page1 + "\n" + page2 + "\n"})
	var lister port.CommentLister = tr
	got, err := lister.Comments(context.Background(), issueID("74"))
	if err != nil {
		t.Fatalf("Comments: %v", err)
	}
	want := []crew.Comment{
		{Author: "ana", Body: "First.", Created: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)},
		{Author: "crew-ops[bot]", App: true, Body: "crew: done.\n", Created: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)},
		{Author: "ana", Body: "Thanks!", Created: time.Date(2026, 10, 2, 8, 30, 0, 0, time.UTC)},
	}
	if !slices.EqualFunc(got, want, func(a, b crew.Comment) bool {
		return a.Author == b.Author && a.App == b.App && a.Body == b.Body && a.Created.Equal(b.Created)
	}) {
		t.Errorf("Comments =\n%+v\nwant\n%+v", got, want)
	}
	if len(gh.calls) != 1 || !slices.Equal(gh.calls[0], listComments) {
		t.Errorf("gh calls = %q, want one listing: %q", gh.calls, listComments)
	}
}

func TestCommentsErrorsAreClassifiedFromTheHTTPStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		stderr string
		want   error // nil: transient
	}{
		"issue gone":     {stderr: "gh: Not Found (HTTP 404)\n", want: port.ErrMovedMeanwhile},
		"a server error": {stderr: "gh: HTTP 502: Bad Gateway\n"},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _ := build(t, reply{prefix: listComments, stderr: tc.stderr})
			_, err := tr.Comments(context.Background(), issueID("74"))
			if err == nil || !strings.Contains(err.Error(), "list the comments of issue #74") {
				t.Fatalf("Comments = %v, want an error naming issue #74", err)
			}
			wantClassified(t, err, tc.want)
		})
	}
}
