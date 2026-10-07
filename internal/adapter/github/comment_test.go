package github

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/thatsnotmynameio/crew/internal/port"
)

func TestCommentPostsTheBodyAsTheWriter(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"a Markdown body": {"## Done\n\n- one\n- two\n", "## Done\n\n- one\n- two\n"},
		"controls stripped, lines kept": {"a\x00b\rc\r\n\x1b[31mred\x1b[0m\nend",
			"a bc\nred\nend"},
	} {
		t.Run(name, func(t *testing.T) {
			var renewed atomic.Int32
			tr, gh := actingTracker(t, &renewed, reply{prefix: commentOn(12), as: asBot, stdout: "901\n"})
			var commenter port.Commenter = tr
			if err := commenter.Comment(context.Background(), issueID("12"), tc.body); err != nil {
				t.Fatalf("Comment: %v", err)
			}
			if len(gh.calls) != 1 {
				t.Fatalf("made gh calls %q, want one", gh.calls)
			}
			posts := gh.commandsTo(commentOn(12)...)
			if len(posts) != 1 || runsAs(posts[0]) != asBot {
				t.Fatalf("posts = %+v, want one as the bot", posts)
			}
			if got := statusBody(t, posts[0].Args); got != tc.want {
				t.Errorf("posted body %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCommentErrorsAreClassifiedFromTheHTTPStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		stderr string
		want   error // nil: transient
	}{
		"issue gone": {stderr: "gh: Not Found (HTTP 404)\n", want: port.ErrMovedMeanwhile},
		"issue locked": {stderr: "gh: Unable to create comment because issue is locked. (HTTP 403)\n",
			want: port.ErrRefused},
		"a server error": {stderr: "gh: HTTP 502: Bad Gateway\n"},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _ := build(t, reply{prefix: commentOn(42), stderr: tc.stderr})
			err := tr.Comment(context.Background(), issueID("42"), "hi")
			if err == nil || !strings.Contains(err.Error(), "comment on issue #42") {
				t.Fatalf("Comment = %v, want an error naming issue #42", err)
			}
			wantClassified(t, err, tc.want)
		})
	}
}
