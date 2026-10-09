// Package securityworkflow tests the scripts that .github/workflows/security.yml
// runs inline, by running them as the workflow holds them.
package securityworkflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// workflow is the part of a workflow file the tests read.
type workflow struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// sarifLog is the part of a SARIF log the tests read.
type sarifLog struct {
	Version string `json:"version"`
	Runs    []struct {
		Tool struct {
			Driver struct {
				Name  string `json:"name"`
				Rules []struct {
					ID string `json:"id"`
				} `json:"rules"`
			} `json:"driver"`
		} `json:"tool"`
		Results []struct {
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
						StartLine int `json:"startLine"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
		} `json:"results"`
	} `json:"runs"`
}

// review is one run of the dependency-review job's sarif and results steps.
type review struct {
	code   int
	stdout string
	sarif  sarifLog
}

// noLicenseChange is the action's invalid-license-changes output when every
// added dependency has an allowed license.
const noLicenseChange = `{"forbidden":[],"unresolved":[],"unlicensed":[]}`

// step returns the run script of the dependency-review job's step name.
func step(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "security.yml"))
	if err != nil {
		t.Fatalf("read the workflow: %v", err)
	}
	var w workflow
	err = yaml.Unmarshal(data, &w)
	if err != nil {
		t.Fatalf("decode the workflow: %v", err)
	}
	for _, s := range w.Jobs["dependency-review"].Steps {
		if s.Name == name {
			return s.Run
		}
	}
	t.Fatalf("the dependency-review job has no step %q", name)

	return ""
}

// runStep runs script as GitHub runs a bash step, with env, and returns its
// exit code and stdout.
func runStep(t *testing.T, script string, env ...string) (int, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", "--noprofile", "--norc", "-eo", "pipefail", "-c", script)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return exit.ExitCode(), stdout.String()
	}
	if err != nil {
		t.Fatalf("run the step: %v", err)
	}

	return 0, stdout.String()
}

// runReview runs the sarif step, then the results step, on the review
// action's two outputs, and decodes the SARIF the first writes.
func runReview(t *testing.T, licenseChanges, vulnerableChanges string) review {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq is not installed")
	}
	temp := "RUNNER_TEMP=" + t.TempDir()
	code, stdout := runStep(t, step(t, "sarif"), temp, "LICENSE_CHANGES="+licenseChanges)
	if code != 0 {
		t.Fatalf("the sarif step exited %d, stdout %q; want 0", code, stdout)
	}
	data, err := os.ReadFile(filepath.Join(strings.TrimPrefix(temp, "RUNNER_TEMP="), "dependency-review.sarif"))
	if err != nil {
		t.Fatalf("read the SARIF: %v", err)
	}
	var log sarifLog
	err = json.Unmarshal(data, &log)
	if err != nil {
		t.Fatalf("decode the SARIF %q: %v", data, err)
	}
	if log.Version != "2.1.0" || len(log.Runs) != 1 || log.Runs[0].Tool.Driver.Name != "dependency-review" {
		t.Fatalf("SARIF %s; want version 2.1.0 and one dependency-review run", data)
	}
	code, stdout = runStep(t, step(t, "results"), temp, "VULNERABLE_CHANGES="+vulnerableChanges)

	return review{code: code, stdout: stdout, sarif: log}
}

func TestPushUploadsAnAnalysisWithNoResultAndPasses(t *testing.T) {
	got := runReview(t, "", "")
	if n := len(got.sarif.Runs[0].Results); n != 0 {
		t.Errorf("%d results; want 0", n)
	}
	if n := len(got.sarif.Runs[0].Tool.Driver.Rules); n != 0 {
		t.Errorf("%d rules; want 0", n)
	}
	if got.code != 0 {
		t.Errorf("the results step exited %d, stdout %q; want 0", got.code, got.stdout)
	}
}

func TestAllowedLicensesPass(t *testing.T) {
	got := runReview(t, noLicenseChange, "[]")
	if n := len(got.sarif.Runs[0].Results); n != 0 {
		t.Errorf("%d results; want 0", n)
	}
	if got.code != 0 {
		t.Errorf("the results step exited %d, stdout %q; want 0", got.code, got.stdout)
	}
}

func TestEachForbiddenLicenseIsAResultAndFails(t *testing.T) {
	changes := `{"forbidden":[
		{"change_type":"added","manifest":"go.mod","ecosystem":"gomod",
		 "name":"github.com/hashicorp/golang-lru/v2","version":"2.0.7","license":"MPL-2.0",
		 "scope":"runtime","vulnerabilities":[]},
		{"change_type":"added","manifest":"go.mod","ecosystem":"gomod",
		 "name":"example.com/nolicense","version":"1.0.0","license":null,
		 "scope":"runtime","vulnerabilities":[]}
	],"unresolved":[],"unlicensed":[]}`
	got := runReview(t, changes, "[]")
	results := got.sarif.Runs[0].Results
	if len(results) != 2 {
		t.Fatalf("%d results; want 2", len(results))
	}
	want := []struct{ rule, message string }{
		{
			"license/github.com/hashicorp/golang-lru/v2",
			"github.com/hashicorp/golang-lru/v2@2.0.7 has the license MPL-2.0, which is not on the allowlist.",
		},
		{
			"license/example.com/nolicense",
			"example.com/nolicense@1.0.0 has the license none, which is not on the allowlist.",
		},
	}
	for i, w := range want {
		r := results[i]
		if r.RuleID != w.rule || r.Level != "error" || r.Message.Text != w.message {
			t.Errorf("result %d is %+v; want rule %q, level error, message %q", i, r, w.rule, w.message)
		}
		if len(r.Locations) != 1 {
			t.Fatalf("result %d has %d locations; want 1", i, len(r.Locations))
		}
		at := r.Locations[0].PhysicalLocation
		if at.ArtifactLocation.URI != "go.mod" || at.Region.StartLine != 1 {
			t.Errorf("result %d is at %+v; want go.mod line 1", i, r.Locations)
		}
	}
	if n := len(got.sarif.Runs[0].Tool.Driver.Rules); n != 2 {
		t.Errorf("%d rules; want 2", n)
	}
	if got.code != 1 {
		t.Errorf("the results step exited %d; want 1", got.code)
	}
	want0 := "::error file=go.mod,line=1::github.com/hashicorp/golang-lru/v2@2.0.7 has the license MPL-2.0"
	if !strings.Contains(got.stdout, want0) {
		t.Errorf("stdout %q; want %q", got.stdout, want0)
	}
}

func TestAVulnerableDependencyFailsWithoutAResult(t *testing.T) {
	changes := `[{"change_type":"added","manifest":"go.mod","ecosystem":"gomod",
		"name":"golang.org/x/net","version":"0.0.1","license":"BSD-3-Clause","scope":"runtime",
		"vulnerabilities":[
			{"severity":"high","advisory_ghsa_id":"GHSA-aaaa-bbbb-cccc","advisory_summary":"one",
			 "advisory_url":"https://github.com/advisories/GHSA-aaaa-bbbb-cccc"},
			{"severity":"low","advisory_ghsa_id":"GHSA-dddd-eeee-ffff","advisory_summary":"two",
			 "advisory_url":"https://github.com/advisories/GHSA-dddd-eeee-ffff"}
		]}]`
	got := runReview(t, noLicenseChange, changes)
	if n := len(got.sarif.Runs[0].Results); n != 0 {
		t.Errorf("%d results; want 0: grype files the advisory", n)
	}
	if got.code != 1 {
		t.Errorf("the results step exited %d; want 1", got.code)
	}
	want := "::error file=go.mod,line=1::golang.org/x/net@0.0.1 has known vulnerabilities: " +
		"GHSA-aaaa-bbbb-cccc, GHSA-dddd-eeee-ffff"
	if !strings.Contains(got.stdout, want) {
		t.Errorf("stdout %q; want %q", got.stdout, want)
	}
}
