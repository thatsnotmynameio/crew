package fakeclaude

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/thatsnotmynameio/crew/acceptance/fakegithub"
)

// args returns claude's headless command line for prompt, as a program
// that reads stream-json runs it.
func args(prompt string) []string {
	return []string{"-p", "--model", "claude-opus-5-5", "--permission-mode", "auto",
		"--output-format", "stream-json", "--verbose", "--", prompt}
}

// run runs one invocation of c with prompt and returns its outcome and
// what it printed.
func run(t *testing.T, c *Claude, inv Invocation) (Outcome, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	o := c.Run(t.Context(), inv, &stdout, &stderr)
	return o, stdout.String(), stderr.String()
}

// event is the part of a stream-json event that a reader of claude's
// output judges a session by.
type event struct {
	Type            string           `json:"type"`
	IsError         bool             `json:"is_error"`
	Result          string           `json:"result"`
	Cost            float64          `json:"total_cost_usd"`
	Turns           int              `json:"num_turns"`
	ModelUsage      map[string]usage `json:"modelUsage"`
	ParentToolUseID *string          `json:"parent_tool_use_id"`
	Message         struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

// usage is one model's tokens in a result event's modelUsage.
type usage struct {
	Input      int `json:"inputTokens"`
	Output     int `json:"outputTokens"`
	CacheRead  int `json:"cacheReadInputTokens"`
	CacheWrite int `json:"cacheCreationInputTokens"`
}

// lines decodes out, stream-json, into its events.
func lines(t *testing.T, out string) []event {
	t.Helper()
	var events []event
	for line := range strings.Lines(out) {
		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %q is not a JSON object: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}

// Covers U3: the first unused script whose key the prompt contains answers,
// printing its events in order and exiting with its code; a second
// invocation with the same key takes the next script.
func TestScriptsWithTheSameKeyAnswerInvocationsInOrder(t *testing.T) {
	c := New(nil)
	c.Script("other key", Succeed("never"))
	c.Script("implement", func(_ context.Context, s *Session) int {
		_ = s.Emit(s.Said("one"), s.Said("two"))
		return 3
	})
	c.Script("implement", Succeed("second"))
	inv := Invocation{Args: args("Please implement issue 4.")}

	o, out, _ := run(t, c, inv)
	if o.Code != 3 || o.Violation != "" {
		t.Fatalf("first outcome = %+v, want code 3 and no violation", o)
	}
	if !strings.Contains(out, `"one"`) || strings.Index(out, `"one"`) > strings.Index(out, `"two"`) {
		t.Fatalf("first output does not print one then two:\n%s", out)
	}
	o, out, _ = run(t, c, inv)
	if o.Code != 0 || !strings.Contains(out, `"result":"second"`) {
		t.Fatalf("second outcome = %+v, output:\n%s\nwant the second script's success", o, out)
	}
}

// Covers U3: an invocation no script matches is a violation naming the
// prompt's first 200 characters, and exits 1.
func TestAnUnmatchedPromptIsAViolationNamingItsFirst200Characters(t *testing.T) {
	c := New(nil)
	c.Script("implement", Succeed("done"))
	prompt := strings.Repeat("a", 200) + strings.Repeat("b", 50)

	o, out, errOut := run(t, c, Invocation{Args: args(prompt)})

	if o.Code != 1 || out != "" {
		t.Fatalf("outcome = %+v, stdout %q; want code 1 and nothing printed", o, out)
	}
	want := "claude -p --model claude-opus-5-5 --permission-mode auto --output-format stream-json --verbose -- '" +
		strings.Repeat("a", 200) + "…(250 chars)' (no script matches the prompt)"
	if o.Violation != want {
		t.Fatalf("violation = %q\nwant        %q", o.Violation, want)
	}
	if !strings.Contains(errOut, want) {
		t.Fatalf("stderr %q does not name the call", errOut)
	}
}

// Covers U3: a command line the fake does not know is a violation, and the
// script it would have matched stays unused.
func TestAnUnknownFlagIsAViolation(t *testing.T) {
	cases := map[string][]string{
		"unknown flag":     {"-p", "--resume", "x", "--output-format", "stream-json", "--verbose", "--", "go"},
		"no print mode":    {"--output-format", "stream-json", "--verbose", "--", "go"},
		"text output":      {"-p", "--output-format", "text", "--verbose", "--", "go"},
		"no verbose":       {"-p", "--output-format", "stream-json", "--", "go"},
		"no prompt":        {"-p", "--output-format", "stream-json", "--verbose"},
		"two prompts":      {"-p", "--output-format", "stream-json", "--verbose", "--", "go", "on"},
		"flag needs value": {"-p", "--verbose", "--model"},
		"add-dir no value": {"-p", "--output-format", "stream-json", "--verbose", "--add-dir"},
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) {
			c := New(nil)
			c.Script("go", Succeed("done"))
			o, _, _ := run(t, c, Invocation{Args: a})
			if o.Code != 1 || o.Violation == "" {
				t.Fatalf("outcome = %+v, want a violation", o)
			}
			if o, _, _ := run(t, c, Invocation{Args: args("go")}); o.Code != 0 {
				t.Fatalf("the script was used up by the refused call: %+v", o)
			}
		})
	}
}

// A session that may write its verdict file's directory is given it with
// --add-dir and a value, before another flag, and finds the file in its
// environment as CREW_VERDICT_FILE.
func TestAddDirWithAValueIsAccepted(t *testing.T) {
	c := New(nil)
	var got Session
	c.Script("go", func(_ context.Context, s *Session) int {
		got = *s
		return 0
	})
	a := []string{"-p", "--model", "claude-opus-5-5", "--add-dir", "/tmp/crew-verdict-1",
		"--permission-mode", "auto", "--output-format", "stream-json", "--verbose", "--", "go"}
	env := map[string]string{"CREW_VERDICT_FILE": "/tmp/crew-verdict-1/verdict"}

	o, _, _ := run(t, c, Invocation{Args: a, Env: env})

	if o.Code != 0 || o.Violation != "" {
		t.Fatalf("outcome = %+v, want the script's success", o)
	}
	if got.Prompt != "go" || got.PermissionMode != "auto" ||
		got.Env["CREW_VERDICT_FILE"] != "/tmp/crew-verdict-1/verdict" {
		t.Fatalf("session = %+v", got)
	}
}

// Covers U3: a script gets the session's working directory, prompt, model,
// permission mode, environment and GitHub handle.
func TestAScriptGetsTheSession(t *testing.T) {
	gh := fakegithub.New("acme", "widgets")
	c := New(gh)
	var got Session
	c.Script("issue 4", func(_ context.Context, s *Session) int {
		got = *s
		return 0
	})
	inv := Invocation{Args: args("Work on issue 4."), Dir: "/work", Env: map[string]string{"GH_CONFIG_DIR": "/gh"}}

	run(t, c, inv)

	if got.Dir != "/work" || got.Prompt != "Work on issue 4." || got.Model != "claude-opus-5-5" ||
		got.PermissionMode != "auto" || got.Env["GH_CONFIG_DIR"] != "/gh" || got.GitHub != gh {
		t.Fatalf("session = %+v", got)
	}
}

// Covers U3 (KTD10): Succeed prints the session's text as its last
// assistant text and a last result event that is not an error, with the
// fixed usage for the model, and exits 0.
func TestSucceedEndsWithASuccessfulResultAndTheFixedUsage(t *testing.T) {
	c := New(nil)
	c.Script("go", Succeed("Opened pull request #12."))

	o, out, _ := run(t, c, Invocation{Args: args("go")})

	events := lines(t, out)
	last, said := events[len(events)-1], events[len(events)-2]
	if o.Code != 0 || last.Type != "result" || last.IsError || last.Result != "Opened pull request #12." {
		t.Fatalf("code %d, last event %+v; want 0 and a successful result", o.Code, last)
	}
	want := usage{Input: InputTokens, Output: OutputTokens, CacheRead: CacheReadTokens, CacheWrite: CacheCreationTokens}
	if last.Cost != Cost || last.Turns != Turns || len(last.ModelUsage) != 1 ||
		last.ModelUsage["claude-opus-5-5"] != want {
		t.Fatalf("usage = %v, %v, %+v; want %v, %v, %+v", last.Cost, last.Turns, last.ModelUsage, Cost, Turns, want)
	}
	if said.Type != "assistant" || said.ParentToolUseID != nil || len(said.Message.Content) != 1 ||
		said.Message.Content[0].Text != "Opened pull request #12." {
		t.Fatalf("the event before the result = %+v, want the session saying its text", said)
	}
}

// Covers U3 (KTD10): Fail ends with an error result carrying the message
// and exits 1; NoResult prints no result event and exits with its code.
func TestFailAndNoResult(t *testing.T) {
	c := New(nil)
	c.Script("fail", Fail("API Error: overloaded"))
	c.Script("cut", NoResult("Reading the issue first.", 0))

	o, out, _ := run(t, c, Invocation{Args: args("fail")})
	events := lines(t, out)
	last := events[len(events)-1]
	if o.Code != 1 || last.Type != "result" || !last.IsError || last.Result != "API Error: overloaded" {
		t.Fatalf("Fail: code %d, last event %+v", o.Code, last)
	}

	o, out, _ = run(t, c, Invocation{Args: args("cut")})
	if o.Code != 0 || strings.Contains(out, `"type":"result"`) || !strings.Contains(out, "Reading the issue first.") {
		t.Fatalf("NoResult: code %d, output:\n%s", o.Code, out)
	}
}
