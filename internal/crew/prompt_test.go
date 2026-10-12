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
	got, err := p.Render(issue)
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
		got, err := p.Render(NewIssue(IssueData{Ref: ref}))
		if err != nil {
			t.Fatalf("Render %s: %v", ref, err)
		}
		if want := "Implement " + ref; got != want {
			t.Errorf("Render = %q, want %q", got, want)
		}
	}
}

func TestParsePromptRefusesABadTemplate(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"does not parse", "{{.Issue.Ref", `parse prompt of action "review": `},
		{"unknown field", "{{.Issue.Number}}", `render prompt of action "review": `},
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
	got, err := Prompt{}.Render(NewIssue(IssueData{Ref: "#1"}))
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
	_, err = p.Render(NewIssue(IssueData{Title: "short"}))
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
