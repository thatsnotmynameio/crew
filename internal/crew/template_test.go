package crew

import (
	"strings"
	"testing"
)

func TestCommentTemplateRendersCrewsData(t *testing.T) {
	text := "{{.Issue.Ref}} {{.Issue.Key}} {{.Issue.Title}} {{.Issue.URL}}: " +
		"{{.Rule}} {{.Action}} {{.Verdict}} {{.Route}} {{.Log}}"
	tmpl, err := ParseCommentTemplate("blocked", text)
	if err != nil {
		t.Fatalf("ParseCommentTemplate: %v", err)
	}
	got, err := tmpl.Render(CommentData{
		Issue: NewIssue(IssueData{
			ID: IssueID{Repository: "R_one", Key: "7"}, Ref: "#7", Title: "Fix it", URL: "https://example.com/7",
		}),
		Rule:    "development",
		Action:  "lfg",
		Verdict: "needs_person",
		Route:   "needs-person",
		Log:     ".crew/logs/issue-7-dev.log",
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "#7 7 Fix it https://example.com/7: development lfg needs_person needs-person .crew/logs/issue-7-dev.log"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestParseCommentTemplateRefusesABadTemplate(t *testing.T) {
	tests := []struct {
		name, text string
		want       []string
	}{
		{"does not parse", "{{.Issue.Ref", []string{`parse comment of route "needs-person": `}},
		{"a reason", "{{.Reason}}", []string{`render comment of route "needs-person": `, "Reason"}},
		{"an unknown issue field", "{{.Issue.Number}}", []string{`render comment of route "needs-person": `, "Number"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCommentTemplate("needs-person", tt.text)
			if err == nil || !strings.HasPrefix(err.Error(), tt.want[0]) {
				t.Fatalf("ParseCommentTemplate(%q) = %v, want an error starting %q", tt.text, err, tt.want[0])
			}
			for _, w := range tt.want[1:] {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("ParseCommentTemplate(%q) = %v, want an error naming %q", tt.text, err, w)
				}
			}
		})
	}
}

func TestCommentTemplateKeepsItsText(t *testing.T) {
	const text = "{{.Action}} stopped as {{.Verdict}}."
	tmpl, err := ParseCommentTemplate("blocked", text)
	if err != nil {
		t.Fatalf("ParseCommentTemplate: %v", err)
	}
	if got := tmpl.Text(); got != text {
		t.Errorf("Text = %q, want %q", got, text)
	}
}

func TestZeroCommentTemplateRendersNothing(t *testing.T) {
	got, err := CommentTemplate{}.Render(CommentData{Issue: NewIssue(IssueData{Ref: "#1"})})
	if err != nil || got != "" {
		t.Fatalf("CommentTemplate{}.Render = %q, %v; want \"\", nil", got, err)
	}
}

func TestCommentTemplateRenderErrorNamesTheRoute(t *testing.T) {
	// Renders for the sample issue's title, and fails on a shorter one.
	tmpl, err := ParseCommentTemplate("blocked", "{{slice .Issue.Title 0 10}}")
	if err != nil {
		t.Fatalf("ParseCommentTemplate: %v", err)
	}
	_, err = tmpl.Render(CommentData{Issue: NewIssue(IssueData{Title: "short"})})
	if err == nil || !strings.HasPrefix(err.Error(), `render comment of route "blocked": `) {
		t.Fatalf("Render = %v, want an error naming the route", err)
	}
}
