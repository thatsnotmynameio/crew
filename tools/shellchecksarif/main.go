// Command shellchecksarif turns shellcheck's json1 output into a SARIF 2.1.0
// log that GitHub code scanning accepts. It reads json1 on stdin and writes
// SARIF on stdout:
//
//	shellcheck --format=json1 tools/*.sh | go run ./tools/shellchecksarif > shellcheck.sarif
//
// shellcheck has no SARIF output of its own. Each comment becomes a result at
// its file and region, and each code a rule SC<code> that links to its wiki
// page. The levels error and warning stay as they are, and info and style
// become note, so code scanning raises an alert for every comment. Input that
// is not a json1 object fails, and writes nothing to stdout.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

// Exit codes.
const (
	exitOK    = 0
	exitError = 1
)

// wiki is where shellcheck documents each code.
const wiki = "https://www.shellcheck.net/wiki/"

// input is shellcheck's json1 output.
type input struct {
	Comments *[]comment `json:"comments"`
}

// comment is one finding in shellcheck's json1 output.
type comment struct {
	File      string `json:"file"`
	Line      int    `json:"line"`
	EndLine   int    `json:"endLine"`
	Column    int    `json:"column"`
	EndColumn int    `json:"endColumn"`
	Level     string `json:"level"`
	Code      int    `json:"code"`
	Message   string `json:"message"`
}

// sarif is a SARIF 2.1.0 log.
type sarif struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

// sarifRun is one tool's run in a SARIF log.
type sarifRun struct {
	Tool    tool     `json:"tool"`
	Results []result `json:"results"`
}

// tool names the tool that made a run.
type tool struct {
	Driver driver `json:"driver"`
}

// driver is the tool and the rules its results cite.
type driver struct {
	Name           string `json:"name"`
	InformationURI string `json:"informationUri"`
	Rules          []rule `json:"rules"`
}

// rule describes one shellcheck code.
type rule struct {
	ID               string `json:"id"`
	ShortDescription text   `json:"shortDescription"`
	FullDescription  text   `json:"fullDescription"`
	Help             help   `json:"help"`
	HelpURI          string `json:"helpUri"`
}

// text is a SARIF message or description.
type text struct {
	Text string `json:"text"`
}

// help is a rule's help, as plain text and as markdown.
type help struct {
	Text     string `json:"text"`
	Markdown string `json:"markdown"`
}

// result is one finding.
type result struct {
	RuleID    string     `json:"ruleId"`
	Level     string     `json:"level"`
	Message   text       `json:"message"`
	Locations []location `json:"locations"`
}

// location is where a finding is.
type location struct {
	PhysicalLocation physicalLocation `json:"physicalLocation"`
}

// physicalLocation is a region of a file.
type physicalLocation struct {
	ArtifactLocation artifactLocation `json:"artifactLocation"`
	Region           region           `json:"region"`
}

// artifactLocation names a file by its path, as shellcheck printed it.
type artifactLocation struct {
	URI string `json:"uri"`
}

// region is a span of lines and columns, its end column exclusive in both
// shellcheck's json1 and SARIF.
type region struct {
	StartLine   int `json:"startLine"`
	EndLine     int `json:"endLine"`
	StartColumn int `json:"startColumn"`
	EndColumn   int `json:"endColumn"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr))
}

// run is main without the process: it returns the exit code.
func run(stdin io.Reader, stdout, stderr io.Writer) int {
	out, err := convert(stdin)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "shellchecksarif: %v\n", err)

		return exitError
	}
	_, err = stdout.Write(out)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "shellchecksarif: write the SARIF: %v\n", err)

		return exitError
	}

	return exitOK
}

// convert reads json1 and returns the SARIF log, indented and ending with a
// newline.
func convert(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read the input: %w", err)
	}
	var in input
	err = json.Unmarshal(data, &in)
	if err != nil {
		return nil, fmt.Errorf("read shellcheck's json1 output: %w", err)
	}
	if in.Comments == nil {
		return nil, errors.New("the input has no comments array: run shellcheck with --format=json1")
	}

	log, err := toSARIF(*in.Comments)
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("write the SARIF: %w", err)
	}

	return append(out, '\n'), nil
}

// toSARIF makes one run of shellcheck's comments, with one rule per code in
// the order the codes first appear.
func toSARIF(comments []comment) (sarif, error) {
	rules := []rule{}
	results := make([]result, 0, len(comments))
	seen := make(map[int]bool)
	for _, c := range comments {
		level, err := sarifLevel(c.Level)
		if err != nil {
			return sarif{}, err
		}
		id := "SC" + strconv.Itoa(c.Code)
		if !seen[c.Code] {
			seen[c.Code] = true
			rules = append(rules, newRule(id))
		}
		results = append(results, result{
			RuleID:  id,
			Level:   level,
			Message: text{Text: c.Message},
			Locations: []location{{PhysicalLocation: physicalLocation{
				ArtifactLocation: artifactLocation{URI: c.File},
				Region: region{
					StartLine:   c.Line,
					EndLine:     c.EndLine,
					StartColumn: c.Column,
					EndColumn:   c.EndColumn,
				},
			}}},
		})
	}

	return sarif{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: tool{Driver: driver{
				Name:           "shellcheck",
				InformationURI: "https://www.shellcheck.net",
				Rules:          rules,
			}},
			Results: results,
		}},
	}, nil
}

// newRule describes the code id, such as SC2086, and links its wiki page.
func newRule(id string) rule {
	uri := wiki + id

	return rule{
		ID:               id,
		ShortDescription: text{Text: "ShellCheck " + id},
		FullDescription:  text{Text: "ShellCheck's check " + id + ", documented at " + uri + "."},
		Help: help{
			Text:     "See " + uri + ".",
			Markdown: "See [" + id + "](" + uri + ").",
		},
		HelpURI: uri,
	}
}

// sarifLevel maps a shellcheck level to a SARIF level. It never returns
// none, which code scanning raises no alert for.
func sarifLevel(level string) (string, error) {
	switch level {
	case "error":
		return "error", nil
	case "warning":
		return "warning", nil
	case "info", "style":
		return "note", nil
	default:
		return "", fmt.Errorf("unknown shellcheck level %q", level)
	}
}
