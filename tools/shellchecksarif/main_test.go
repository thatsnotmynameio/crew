package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sarifLog is the part of a SARIF log the tests read.
type sarifLog struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []struct {
		Tool struct {
			Driver struct {
				Name  string `json:"name"`
				Rules []struct {
					ID               string `json:"id"`
					ShortDescription struct {
						Text string `json:"text"`
					} `json:"shortDescription"`
					HelpURI string `json:"helpUri"`
				} `json:"rules"`
			} `json:"driver"`
		} `json:"tool"`
		Results []sarifTestResult `json:"results"`
	} `json:"runs"`
}

// sarifTestResult is the part of a SARIF result the tests read.
type sarifTestResult struct {
	RuleID  string `json:"ruleId"`
	Level   string `json:"level"`
	Message struct {
		Text string `json:"text"`
	} `json:"message"`
	Locations []struct {
		PhysicalLocation struct {
			ArtifactLocation struct {
				URI string `json:"uri"`
			} `json:"artifactLocation"`
			Region struct {
				StartLine   int `json:"startLine"`
				EndLine     int `json:"endLine"`
				StartColumn int `json:"startColumn"`
				EndColumn   int `json:"endColumn"`
			} `json:"region"`
		} `json:"physicalLocation"`
	} `json:"locations"`
}

// runOn runs the command on input and returns its exit code, stdout and
// stderr.
func runOn(t *testing.T, input string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(strings.NewReader(input), &stdout, &stderr)

	return code, stdout.String(), stderr.String()
}

// convertLog runs the command on input, which must succeed, and decodes the
// SARIF it writes.
func convertLog(t *testing.T, input string) sarifLog {
	t.Helper()
	code, stdout, stderr := runOn(t, input)
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q; want 0", code, stderr)
	}
	var log sarifLog
	err := json.Unmarshal([]byte(stdout), &log)
	if err != nil {
		t.Fatalf("decode the SARIF %q: %v", stdout, err)
	}
	if len(log.Runs) != 1 {
		t.Fatalf("%d runs; want 1", len(log.Runs))
	}

	return log
}

func TestCommentsInTwoFilesBecomeResultsAndRules(t *testing.T) {
	input := `{"comments":[
		{"file":"tools/a.sh","line":3,"endLine":3,"column":6,"endColumn":8,"level":"warning","code":2086,
		 "message":"Double quote to prevent globbing and word splitting.","fix":null},
		{"file":"b.sh","line":2,"endLine":4,"column":1,"endColumn":12,"level":"error","code":1090,
		 "message":"ShellCheck can't follow non-constant source.","fix":null}]}`
	got := convertLog(t, input).Runs[0]

	rules := got.Tool.Driver.Rules
	if len(rules) != 2 || rules[0].ID != "SC2086" || rules[1].ID != "SC1090" {
		t.Fatalf("rules %+v; want SC2086 and SC1090", rules)
	}
	if rules[1].HelpURI != "https://www.shellcheck.net/wiki/SC1090" || rules[1].ShortDescription.Text == "" {
		t.Fatalf("rule %+v; want SC1090's wiki page and a short description", rules[1])
	}
	if len(got.Results) != 2 {
		t.Fatalf("%d results; want 2", len(got.Results))
	}
	checkResult(t, got.Results[0], "SC2086", "warning", "tools/a.sh", [4]int{3, 3, 6, 8})
	checkResult(t, got.Results[1], "SC1090", "error", "b.sh", [4]int{2, 4, 1, 12})
	if got.Results[1].Message.Text != "ShellCheck can't follow non-constant source." {
		t.Fatalf("message %q; want the comment's message", got.Results[1].Message.Text)
	}
}

// checkResult fails unless result has the rule, level, path and region
// (start line, end line, start column, end column) given.
func checkResult(t *testing.T, result sarifTestResult, rule, level, path string, region [4]int) {
	t.Helper()
	if result.RuleID != rule || result.Level != level || len(result.Locations) != 1 {
		t.Fatalf("result %+v; want rule %s, level %s and one location", result, rule, level)
	}
	location := result.Locations[0].PhysicalLocation
	got := [4]int{location.Region.StartLine, location.Region.EndLine,
		location.Region.StartColumn, location.Region.EndColumn}
	if location.ArtifactLocation.URI != path || got != region {
		t.Fatalf("location %s %v; want %s %v", location.ArtifactLocation.URI, got, path, region)
	}
}

func TestStyleAndInfoBecomeNote(t *testing.T) {
	input := `{"comments":[
		{"file":"a.sh","line":1,"endLine":1,"column":1,"endColumn":2,"level":"style","code":2006,"message":"m"},
		{"file":"a.sh","line":2,"endLine":2,"column":1,"endColumn":2,"level":"info","code":2086,"message":"m"}]}`
	results := convertLog(t, input).Runs[0].Results
	if len(results) != 2 || results[0].Level != "note" || results[1].Level != "note" {
		t.Fatalf("results %+v; want two at level note", results)
	}
}

func TestOneRulePerCode(t *testing.T) {
	comment := `{"file":"a.sh","line":1,"endLine":1,"column":1,"endColumn":2,"level":"info","code":2086,"message":"m"}`
	got := convertLog(t, `{"comments":[`+comment+`,`+comment+`,`+comment+`]}`).Runs[0]
	if len(got.Tool.Driver.Rules) != 1 || len(got.Results) != 3 {
		t.Fatalf("%d rules and %d results; want 1 and 3", len(got.Tool.Driver.Rules), len(got.Results))
	}
}

func TestNoCommentsIsAnEmptyRun(t *testing.T) {
	log := convertLog(t, `{"comments": []}`)
	_, stdout, _ := runOn(t, `{"comments": []}`)
	if log.Version != "2.1.0" || log.Runs[0].Tool.Driver.Name != "shellcheck" || len(log.Runs[0].Results) != 0 {
		t.Fatalf("log %+v; want version 2.1.0, one shellcheck run and no results", log)
	}
	if !strings.Contains(stdout, `"results": []`) {
		t.Fatalf("SARIF %q; want an empty results array, not a missing one", stdout)
	}
}

func TestBadInputFails(t *testing.T) {
	for name, input := range map[string]string{
		"not JSON":      "In a.sh line 1: oops",
		"an array":      `[{"file":"a.sh","line":1,"level":"error","code":1}]`,
		"no comments":   `{}`,
		"null":          `null`,
		"unknown level": `{"comments":[{"file":"a.sh","line":1,"level":"fatal","code":1,"message":"m"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := runOn(t, input)
			if code == 0 || stdout != "" || !strings.HasPrefix(stderr, "shellchecksarif: ") {
				t.Fatalf("exit code %d, stdout %q, stderr %q; want a failure, no SARIF and an error", code, stdout, stderr)
			}
		})
	}
}

func TestGolden(t *testing.T) {
	input, err := os.ReadFile(filepath.Join("testdata", "golden.json1"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "golden.sarif"))
	if err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runOn(t, string(input))
	if code != 0 || stdout != string(want) {
		t.Fatalf("exit code %d, stderr %q, SARIF:\n%s\nwant testdata/golden.sarif:\n%s", code, stderr, stdout, want)
	}
}
