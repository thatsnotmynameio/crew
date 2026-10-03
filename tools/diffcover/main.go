// Command diffcover checks the test coverage of the Go lines a pull request
// adds or changes. It reads a coverage profile and a unified diff on stdin,
// and fails when fewer than -threshold percent of the changed coverable lines
// are covered:
//
//	git diff -U0 origin/main...HEAD | go run ./tools/diffcover -profile coverage.out
//
// A change with no coverable lines, such as one to docs, workflows or tests
// only, passes. Codacy's own diff-coverage gate fails such a change, which is
// why crew checks this itself.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// defaultThreshold is the share of changed coverable lines that must be
// covered, in percent.
const defaultThreshold = 90.0

// percent turns a ratio into a percentage.
const percent = 100.0

// maxDiffLine is the longest diff line read, in bytes.
const maxDiffLine = 1 << 20

// Exit codes.
const (
	exitPass  = 0
	exitBelow = 1
	exitError = 2
)

// lineKey is one line of one file, by its path relative to the repository.
type lineKey struct {
	file string
	line int
}

// result is what the check found.
type result struct {
	coverable int
	covered   int
}

// Pct is the covered share of the coverable changed lines, in percent.
func (r result) Pct() float64 {
	if r.coverable == 0 {
		return percent
	}

	return percent * float64(r.covered) / float64(r.coverable)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run is main without the process: it returns the exit code.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("diffcover", flag.ContinueOnError)
	flags.SetOutput(stderr)
	profilePath := flags.String("profile", "coverage.out",
		"the coverage profile written by go test -coverprofile")
	modulePath := flags.String("module", "github.com/thatsnotmynameio/crew",
		"the module path that prefixes the profile's file names")
	threshold := flags.Float64("threshold", defaultThreshold,
		"the minimum covered share of changed coverable lines, in percent")

	err := flags.Parse(args)
	if err != nil {
		return exitError
	}

	res, err := check(*profilePath, *modulePath, stdin)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "diffcover: %v\n", err)

		return exitError
	}

	return report(res, *threshold, stdout)
}

// report prints the result and returns the exit code.
func report(res result, threshold float64, stdout io.Writer) int {
	if res.coverable == 0 {
		_, _ = fmt.Fprintln(stdout, "diffcover: no coverable lines changed")

		return exitPass
	}

	_, _ = fmt.Fprintf(stdout, "diffcover: %d of %d changed coverable lines covered (%.1f%%, minimum %.1f%%)\n",
		res.covered, res.coverable, res.Pct(), threshold)
	if res.Pct() < threshold {
		return exitBelow
	}

	return exitPass
}

// check reads the profile at profilePath and the diff, and counts the
// changed lines that are coverable and covered.
func check(profilePath, modulePath string, diff io.Reader) (result, error) {
	profile, err := os.Open(profilePath)
	if err != nil {
		return result{}, fmt.Errorf("open the coverage profile: %w", err)
	}
	defer func() { _ = profile.Close() }()

	lines, err := parseProfile(profile, modulePath)
	if err != nil {
		return result{}, fmt.Errorf("read the coverage profile %s: %w", profilePath, err)
	}

	changed, err := parseDiff(diff)
	if err != nil {
		return result{}, fmt.Errorf("read the diff: %w", err)
	}

	var res result
	for _, key := range changed {
		count, ok := lines[key]
		if !ok {
			continue
		}
		res.coverable++
		if count > 0 {
			res.covered++
		}
	}

	return res, nil
}

// parseProfile maps every line inside a statement block of the profile to
// the highest count any block covering it has. With -coverpkg each test
// binary writes the same block again, so a line is covered when any copy of
// its block ran.
func parseProfile(r io.Reader, modulePath string) (map[lineKey]int, error) {
	lines := make(map[lineKey]int)
	scanner := bufio.NewScanner(r)
	first := true
	for scanner.Scan() {
		text := scanner.Text()
		if first {
			first = false
			if !strings.HasPrefix(text, "mode: ") {
				return nil, errors.New("it does not start with a mode line")
			}

			continue
		}
		if text == "" {
			continue
		}
		err := addBlock(lines, text, modulePath)
		if err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	if first {
		return nil, errors.New("it is empty")
	}

	return lines, nil
}

// addBlock records one profile line, such as
// "github.com/x/y/a.go:10.2,12.3 2 1": file, start line and column, end line
// and column, statements, count.
func addBlock(lines map[lineKey]int, text, modulePath string) error {
	colon := strings.LastIndex(text, ":")
	fields := strings.Fields(text[colon+1:])
	if colon < 0 || len(fields) != 3 {
		return fmt.Errorf("malformed block %q", text)
	}
	file := strings.TrimPrefix(strings.TrimPrefix(text[:colon], modulePath), "/")
	start, end, err := blockLines(fields[0])
	if err != nil {
		return fmt.Errorf("malformed block %q: %w", text, err)
	}
	statements, err := strconv.Atoi(fields[1])
	if err != nil {
		return fmt.Errorf("malformed block %q: %w", text, err)
	}
	count, err := strconv.Atoi(fields[2])
	if err != nil {
		return fmt.Errorf("malformed block %q: %w", text, err)
	}
	if statements == 0 {
		return nil
	}
	for line := start; line <= end; line++ {
		key := lineKey{file: file, line: line}
		if old, ok := lines[key]; !ok || count > old {
			lines[key] = count
		}
	}

	return nil
}

// blockLines reads "10.2,12.3" as the lines 10 to 12.
func blockLines(span string) (int, int, error) {
	from, to, ok := strings.Cut(span, ",")
	if !ok {
		return 0, 0, errors.New("no comma in the span")
	}
	startLine, _, _ := strings.Cut(from, ".")
	start, err := strconv.Atoi(startLine)
	if err != nil {
		return 0, 0, fmt.Errorf("start line: %w", err)
	}
	endLine, _, _ := strings.Cut(to, ".")
	end, err := strconv.Atoi(endLine)
	if err != nil {
		return 0, 0, fmt.Errorf("end line: %w", err)
	}

	return start, end, nil
}

// parseDiff lists the lines a unified diff adds or changes, by their number
// in the new file. Removed lines leave nothing to cover.
func parseDiff(r io.Reader) ([]lineKey, error) {
	var changed []lineKey
	var file string
	next := 0
	afterOld := false
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxDiffLine)
	for scanner.Scan() {
		text := scanner.Text()
		// "+++ b/path" names the new file only right after "--- a/path"; an
		// added line can start with "++" too.
		header := afterOld && strings.HasPrefix(text, "+++ ")
		afterOld = strings.HasPrefix(text, "--- ")
		switch {
		case header:
			file = strings.TrimPrefix(strings.TrimPrefix(text, "+++ "), "b/")
			next = 0
		case strings.HasPrefix(text, "@@ "):
			start, err := hunkStart(text)
			if err != nil {
				return nil, err
			}
			next = start
		case strings.HasPrefix(text, "+") && file != "" && next > 0:
			changed = append(changed, lineKey{file: file, line: next})
			next++
		case strings.HasPrefix(text, " ") && next > 0:
			next++
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	return changed, nil
}

// hunkStart reads the new file's first line from a hunk header such as
// "@@ -3,0 +4,2 @@".
func hunkStart(header string) (int, error) {
	fields := strings.Fields(header)
	if len(fields) < 3 || !strings.HasPrefix(fields[2], "+") {
		return 0, fmt.Errorf("malformed hunk header %q", header)
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(fields[2], "+"), ",")
	start, err := strconv.Atoi(first)
	if err != nil {
		return 0, fmt.Errorf("malformed hunk header %q: %w", header, err)
	}

	return start, nil
}
