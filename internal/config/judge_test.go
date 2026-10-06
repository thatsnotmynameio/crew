package config_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// stubCurl stands in for curl on the judge's PATH. It counts its calls,
// keeps its arguments, the headers it read from stdin and the request it
// was given, then writes $STUB_DIR/body to its -o file and prints
// $STUB_CODE, as curl's -w '%{http_code}' would; 000 is no answer.
const stubCurl = `#!/bin/sh
echo call >> "$STUB_DIR/calls"
printf '%s\n' "$@" > "$STUB_DIR/args"
cat > "$STUB_DIR/headers"
out= data=
while [ $# -gt 0 ]; do
  case $1 in
  -o) out=$2; shift ;;
  --data-binary) data=${2#@}; shift ;;
  esac
  shift
done
cp "$data" "$STUB_DIR/request"
[ "$STUB_CODE" = 000 ] && exit 7
cp "$STUB_DIR/body" "$out"
printf '%s' "$STUB_CODE"
`

// stubSleep records how long the judge waited instead of waiting.
const stubSleep = `#!/bin/sh
echo "$1" >> "$STUB_DIR/slept"
`

// judgeRun is one run of the session-finished check against the stubs.
type judgeRun struct {
	noKey bool   // TYPESAFE_API_KEY left unset
	last  string // the session's last message
	code  string // the HTTP status TypeSafe answers, or 000
	body  string // TypeSafe's answer
}

// judged is how a judgeRun ended.
type judged struct {
	exit    int
	line    string // the last line it printed: its reason
	calls   int
	slept   []string
	args    string
	headers string
	request []byte
}

// stubKeyValue is the TYPESAFE_API_KEY the judge runs with.
const stubKeyValue = "ts-test-key-4c1d"

// judgePrompt is the prompt the judged session started with.
const judgePrompt = "/compound-engineering:lfg #9\n\nThe pull request body must contain the line `Closes #9`."

// judgeScript returns the script of session-finished, as development's lfg
// action names it in crew's own config.
func judgeScript(t *testing.T) string {
	t.Helper()
	script := checkScript(loadOwn(t).Rules[3].Actions[0], "session-finished")
	if script == "" {
		t.Fatal("development's lfg action names no session-finished check")
	}
	return script
}

// runJudge runs script as the shell checker would, with stubs for curl and
// sleep first on its PATH, and the real jq.
func runJudge(t *testing.T, script string, r judgeRun) judged {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		if os.Getenv("CI") == "" {
			t.Skip("jq is not on the PATH; the judge needs it")
		}
		t.Fatal("jq is not on the PATH; the judge needs it")
	}
	dir, bin := t.TempDir(), t.TempDir()
	for name, text := range map[string]string{
		filepath.Join(bin, "curl"): stubCurl, filepath.Join(bin, "sleep"): stubSleep,
		filepath.Join(dir, "body"): r.body, filepath.Join(dir, "prompt"): judgePrompt,
		filepath.Join(dir, "last-message"): r.last,
	} {
		if err := os.WriteFile(name, []byte(text), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"STUB_DIR=" + dir, "STUB_CODE=" + r.code,
		"CREW_ACTION=lfg",
		"CREW_PROMPT_FILE=" + filepath.Join(dir, "prompt"),
		"CREW_LAST_MESSAGE_FILE=" + filepath.Join(dir, "last-message"),
	}
	if !r.noKey {
		env = append(env, "TYPESAFE_API_KEY="+stubKeyValue)
	}
	cmd := exec.CommandContext(t.Context(), "sh", "-c", script)
	cmd.Dir, cmd.Env = t.TempDir(), env
	out, err := cmd.CombinedOutput()
	var got judged
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		got.exit = exit.ExitCode()
	case err != nil:
		t.Fatalf("run the judge: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	got.line = lines[len(lines)-1]
	read := func(name string) string {
		data, _ := os.ReadFile(filepath.Join(dir, name)) // a file the stub never wrote reads empty
		return string(data)
	}
	got.calls = strings.Count(read("calls"), "call")
	got.slept = strings.Fields(read("slept"))
	got.args, got.headers, got.request = read("args"), read("headers"), []byte(read("request"))
	return got
}

// answer is TypeSafe's answer giving each option of Choice a and b its
// probability.
func answer(t *testing.T, a, b map[string]float64) string {
	t.Helper()
	choice := func(p map[string]float64) map[string]any {
		return map[string]any{"type": "choice", "probabilities": p}
	}
	data, err := json.Marshal(map[string]any{
		"model":   "jev-1.13.0",
		"answers": map[string]any{"a": choice(a), "b": choice(b)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// both is answer with the same probabilities in both orders.
func both(t *testing.T, p map[string]float64) string {
	t.Helper()
	return answer(t, p, p)
}

// verdictCase is an answer of TypeSafe's and how the judge ends on it.
type verdictCase struct {
	name string
	body string
	exit int
	line string
}

// verdictCases are the answers the judge decides on, as R8 has it.
func verdictCases(t *testing.T) []verdictCase {
	t.Helper()
	return []verdictCase{
		{
			// Covers AE1.
			"unfinished", both(t, map[string]float64{"done": 0, "unfinished": 1, "needs_person": 0, "stopped": 0}),
			1, "unfinished (1.00)",
		},
		{
			// Covers AE2.
			"done",
			both(t, map[string]float64{"done": 0.97, "unfinished": 0.01, "needs_person": 0.01, "stopped": 0.01}),
			0, "done (0.97)",
		},
		{
			// Covers AE4.
			"needs a person",
			both(t, map[string]float64{"done": 0.03, "unfinished": 0.01, "needs_person": 0.95, "stopped": 0.01}),
			0, "needs a person (0.95)",
		},
		{
			"needs a person below its threshold",
			both(t, map[string]float64{"done": 0.4, "unfinished": 0.05, "needs_person": 0.5, "stopped": 0.05}),
			0, "done (0.40)",
		},
		{
			"stopped", both(t, map[string]float64{"done": 0.3, "unfinished": 0.1, "needs_person": 0, "stopped": 0.6}),
			1, "stopped (0.60)",
		},
		{
			"unfinished and stopped reach the threshold together",
			both(t, map[string]float64{"done": 0.4, "unfinished": 0.3, "needs_person": 0, "stopped": 0.3}),
			1, "unfinished (0.30)",
		},
		{
			"unfinished and stopped below the threshold",
			both(t, map[string]float64{"done": 0.5, "unfinished": 0.25, "needs_person": 0, "stopped": 0.25}),
			0, "done (0.50)",
		},
		{
			"the two orders are averaged",
			answer(t, map[string]float64{"done": 0.2, "unfinished": 0.8, "needs_person": 0, "stopped": 0},
				map[string]float64{"done": 0.6, "unfinished": 0.4, "needs_person": 0, "stopped": 0}),
			1, "unfinished (0.60)",
		},
	}
}

// R7, R8: the judge fails on unfinished or stopped and passes otherwise,
// echoing the outcome and its probability averaged over both orders.
func TestTheJudgeFailsAnUnfinishedOrStoppedSession(t *testing.T) {
	script := judgeScript(t)
	for _, tt := range verdictCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			got := runJudge(t, script, judgeRun{last: "PR #20 is open.\nMerging is yours.", code: "200", body: tt.body})
			if got.exit != tt.exit || got.line != tt.line || got.calls != 1 {
				t.Errorf("exit %d, %q after %d calls; want exit %d, %q after one",
					got.exit, got.line, got.calls, tt.exit, tt.line)
			}
		})
	}
}

// unjudgedCase is a run in which the judge cannot judge, or has no key,
// and how it ends: its exit, its reason, its calls to curl and its waits.
type unjudgedCase struct {
	name  string
	run   judgeRun
	exit  int
	line  string
	calls int
	slept []string
}

// unjudgedCases are the runs R9 and R10 cover.
func unjudgedCases(t *testing.T) []unjudgedCase {
	t.Helper()
	done := both(t, map[string]float64{"done": 1, "unfinished": 0, "needs_person": 0, "stopped": 0})
	return []unjudgedCase{
		{
			// Covers AE5.
			"rate limited on every try", judgeRun{last: "Done.", code: "429", body: "{}"},
			0, "not judged: TypeSafe answered HTTP 429", 3, []string{"2", "4"},
		},
		{
			"overloaded on every try", judgeRun{last: "Done.", code: "529", body: "{}"},
			0, "not judged: TypeSafe answered HTTP 529", 3, []string{"2", "4"},
		},
		{
			"no answer", judgeRun{last: "Done.", code: "000"},
			0, "not judged: TypeSafe could not be reached", 3, []string{"2", "4"},
		},
		{
			"refused", judgeRun{last: "Done.", code: "401", body: `{"error":"bad key"}`},
			0, "not judged: TypeSafe answered HTTP 401", 1, nil,
		},
		{
			"an answer that is not JSON", judgeRun{last: "Done.", code: "200", body: "<html>"},
			0, "not judged: TypeSafe's answer could not be read", 1, nil,
		},
		{
			"an answer without probabilities", judgeRun{last: "Done.", code: "200", body: `{"answers":{}}`},
			0, "not judged: TypeSafe's answer could not be read", 1, nil,
		},
		{
			// Covers AE6.
			"no key", judgeRun{noKey: true, last: "Done.", code: "200", body: done},
			1, "TYPESAFE_API_KEY is not set: the judge cannot ask Jev", 0, nil,
		},
		{
			// Covers AE7.
			"an empty last message", judgeRun{code: "200", body: done},
			0, "not judged: the session's last message is empty", 0, nil,
		},
		{
			"a blank last message", judgeRun{last: " \n\t\n", code: "200", body: done},
			0, "not judged: the session's last message is empty", 0, nil,
		},
	}
}

// R9, R10: the judge passes, saying so, when it cannot judge, and fails
// when it has no key.
func TestTheJudgePassesWhenItCannotJudgeAndFailsWithoutAKey(t *testing.T) {
	script := judgeScript(t)
	for _, tt := range unjudgedCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			got := runJudge(t, script, tt.run)
			if got.exit != tt.exit || got.line != tt.line || got.calls != tt.calls || !slices.Equal(got.slept, tt.slept) {
				t.Errorf("exit %d, %q after %d calls, slept %q; want exit %d, %q after %d, slept %q",
					got.exit, got.line, got.calls, got.slept, tt.exit, tt.line, tt.calls, tt.slept)
			}
		})
	}
}

// R7, R11: the judge asks jev-1.13.0 one Choice in two option orders, with
// the action, the prompt and the last message as they are, and keeps the
// key out of curl's arguments.
func TestTheJudgeAsksJevOneChoiceInTwoOrders(t *testing.T) {
	last := "The suite is still running in the background; I'll pick up when it reports back.\n\"quoted\" and `ticks`"
	got := runJudge(t, judgeScript(t), judgeRun{last: last, code: "200",
		body: both(t, map[string]float64{"done": 1, "unfinished": 0, "needs_person": 0, "stopped": 0})})

	var request struct {
		Model     string            `json:"model"`
		State     map[string]string `json:"state"`
		Questions map[string]struct {
			Type     string          `json:"type"`
			Criteria json.RawMessage `json:"criteria"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(got.request, &request); err != nil {
		t.Fatalf("request %q: %v", got.request, err)
	}
	if request.Model != "jev-1.13.0" {
		t.Errorf("model = %q, want the pinned jev-1.13.0", request.Model)
	}
	wantState := map[string]string{"action": "lfg", "prompt": judgePrompt, "last_message": last}
	if !reflect.DeepEqual(request.State, wantState) {
		t.Errorf("state = %q, want %q", request.State, wantState)
	}
	a, b := request.Questions["a"], request.Questions["b"]
	if len(request.Questions) != 2 || a.Type != "choice" || b.Type != "choice" {
		t.Fatalf("questions = %+v, want Choices a and b", request.Questions)
	}
	order := []string{"done", "unfinished", "needs_person", "stopped"}
	reversed := slices.Clone(order)
	slices.Reverse(reversed)
	if got := keysInOrder(t, a.Criteria); !slices.Equal(got, order) {
		t.Errorf("a's options = %q, want %q", got, order)
	}
	if got := keysInOrder(t, b.Criteria); !slices.Equal(got, reversed) {
		t.Errorf("b's options = %q, want %q", got, reversed)
	}
	if strings.Contains(got.args, stubKeyValue) || !strings.Contains(got.headers, "Authorization: Bearer "+stubKeyValue) {
		t.Errorf("curl got the key in its arguments %q or not in its headers %q", got.args, got.headers)
	}
}

// keysInOrder returns the keys of the JSON object data, in order.
func keysInOrder(t *testing.T, data json.RawMessage) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		key, ok := token.(string)
		if !ok {
			t.Fatalf("key %v is not a string", token)
		}
		keys = append(keys, key)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}
