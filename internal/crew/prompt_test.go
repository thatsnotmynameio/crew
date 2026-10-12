package crew

import (
	"strings"
	"testing"
)

func TestPromptRendersTheIssuesFields(t *testing.T) {
	p, err := ParsePrompt("implement", "{{.Issue.Ref}} {{.Issue.Key}} {{.Issue.Title}} {{.Issue.URL}}")
	if err != nil {
		t.Fatalf("ParsePrompt: %v", err)
	}
	issue := NewIssue(IssueData{
		ID: IssueID{Repository: "R_one", Key: "7"}, Ref: "#7", Title: "Fix it", URL: "https://example.com/7",
	})
	got, err := p.Render(PromptData{Issue: issue})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "#7 7 Fix it https://example.com/7"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestPromptRendersEachIssueItIsGiven(t *testing.T) {
	p, err := ParsePrompt("implement", "Implement {{.Issue.Ref}}")
	if err != nil {
		t.Fatalf("ParsePrompt: %v", err)
	}
	for _, ref := range []string{"#1", "#2"} {
		got, err := p.Render(PromptData{Issue: NewIssue(IssueData{Ref: ref})})
		if err != nil {
			t.Fatalf("Render %s: %v", ref, err)
		}
		if want := "Implement " + ref; got != want {
			t.Errorf("Render = %q, want %q", got, want)
		}
	}
}

func TestPromptRendersTheBase(t *testing.T) {
	p, err := ParsePrompt("implement", "Open the pull request on {{.Base}}")
	if err != nil {
		t.Fatalf("ParsePrompt: %v", err)
	}
	got, err := p.Render(PromptData{Issue: NewIssue(IssueData{Ref: "#7"}), Base: "trunk"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "Open the pull request on trunk"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestPromptRendersTheStackOnlyForAStackedRun(t *testing.T) {
	p, err := ParsePrompt("implement", "{{if .Stack.Stacked}}{{.Stack.PullRequest.URL}}{{end}}")
	if err != nil {
		t.Fatalf("ParsePrompt: %v", err)
	}
	issue := NewIssue(IssueData{Ref: "#7"})
	stack := Stack{PullRequestRef: "#45", PullRequestURL: "https://example.com/pull/45", Branch: "crew/issue-6-implement"}
	tests := []struct {
		name  string
		stack Optional[Stack]
		want  string
	}{
		{"stacked", Some(stack), "https://example.com/pull/45"},
		{"unstacked", Optional[Stack]{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := p.Render(PromptData{Issue: issue, Stack: tt.stack})
			if err != nil || got != tt.want {
				t.Errorf("Render = %q, %v; want %q, nil", got, err, tt.want)
			}
		})
	}
}

func TestPromptRendersEveryStackField(t *testing.T) {
	p, err := ParsePrompt("implement",
		"{{.Stack.Stacked}} {{.Stack.Blocker.Ref}} {{.Stack.Blocker.URL}} "+
			"{{.Stack.PullRequest.Ref}} {{.Stack.PullRequest.URL}} {{.Stack.Branch}}")
	if err != nil {
		t.Fatalf("ParsePrompt: %v", err)
	}
	stack := Stack{
		Blocker: IssueID{Key: "6"}, BlockerRef: "#6", BlockerURL: "https://example.com/issues/6",
		PullRequestRef: "#45", PullRequestURL: "https://example.com/pull/45", Branch: "crew/issue-6-implement",
	}
	got, err := p.Render(PromptData{Issue: NewIssue(IssueData{Ref: "#7"}), Stack: Some(stack)})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := "true #6 https://example.com/issues/6 #45 https://example.com/pull/45 crew/issue-6-implement"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
	got, err = p.Render(PromptData{Issue: NewIssue(IssueData{Ref: "#7"})})
	if want := "false     "; err != nil || got != want {
		t.Errorf("Render unstacked = %q, %v; want %q, nil", got, err, want)
	}
}

func TestParsePromptRefusesABadTemplate(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"does not parse", "{{.Issue.Ref", `parse prompt of action "review": `},
		{"unknown field", "{{.Issue.Number}}", `render prompt of action "review": `},
		{
			"typo in a stacked branch", "{{if .Stack.Stacked}}{{.Stack.PullRequest.Ulr}}{{end}}",
			`render prompt of action "review": `,
		},
		{"unknown stack field", "{{.Stack.Blocker.Number}}", `render prompt of action "review": `},
		{
			"typo without a base", "{{if .Base}}on {{.Base}}{{else}}{{.Issue.Nmber}}{{end}}",
			`render prompt of action "review": `,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePrompt("review", tt.text)
			if err == nil || !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("ParsePrompt(%q) = %v, want an error starting %q", tt.text, err, tt.want)
			}
		})
	}
}

func TestZeroPromptRendersNothing(t *testing.T) {
	got, err := Prompt{}.Render(PromptData{Issue: NewIssue(IssueData{Ref: "#1"})})
	if err != nil || got != "" {
		t.Fatalf("Prompt{}.Render = %q, %v; want \"\", nil", got, err)
	}
	if text := (Prompt{}).Text(); text != "" {
		t.Errorf("Prompt{}.Text = %q, want \"\"", text)
	}
}

func TestPromptKeepsItsText(t *testing.T) {
	const text = "Implement {{.Issue.Ref}}: {{.Issue.Title}}"
	p, err := ParsePrompt("implement", text)
	if err != nil {
		t.Fatalf("ParsePrompt: %v", err)
	}
	if got := p.Text(); got != text {
		t.Errorf("Text = %q, want %q", got, text)
	}
}

func TestPromptRenderErrorNamesTheAction(t *testing.T) {
	// Renders for the sample issue's title, and fails on a shorter one.
	p, err := ParsePrompt("implement", "{{index .Issue.Title 11}}")
	if err != nil {
		t.Fatalf("ParsePrompt: %v", err)
	}
	_, err = p.Render(PromptData{Issue: NewIssue(IssueData{Title: "short"})})
	if err == nil || !strings.HasPrefix(err.Error(), `render prompt of action "implement": `) {
		t.Fatalf("Render = %v, want an error naming the action", err)
	}
}

// Covers KTD8: a prompt's hash is the SHA-256, in hex, of its template as
// the config wrote it, whatever action it belongs to.
func TestPromptHashIsTheSHA256OfItsTemplate(t *testing.T) {
	parse := func(action ActionName, text string) Prompt {
		t.Helper()
		p, err := ParsePrompt(action, text)
		if err != nil {
			t.Fatalf("ParsePrompt: %v", err)
		}
		return p
	}
	review, again := parse("review", "Review {{.Issue.Ref}}"), parse("review again", "Review {{.Issue.Ref}}")
	edited := parse("review", "Review {{.Issue.Ref}} closely")

	// echo -n 'Review {{.Issue.Ref}}' | sha256sum
	if want := "4bbac29b2d5ab5a7a5fc60ed83a1a390c9494c73b8bfaa15108a2ebd022d05ec"; review.Hash() != want {
		t.Errorf("Hash() = %q, want %q", review.Hash(), want)
	}
	if again.Hash() != review.Hash() {
		t.Errorf("the same template in two actions hashes %q and %q", review.Hash(), again.Hash())
	}
	if edited.Hash() == review.Hash() {
		t.Errorf("an edited template keeps the hash %q", edited.Hash())
	}
}
