#!/bin/sh
# Tests the prompts and shell actions of this repository's own
# .crew/config.yaml: the refine prompt agrees with the /cw-split-plan and
# /cw-rank-blockers skills it runs, the rules run the shell actions after
# their sessions, and the session-finished shell action judges a session as
# it should, against stub curl and sleep commands first on PATH, so it runs
# without TypeSafe. crew's own tests never read this config, so a change to
# it runs this by hand, from anywhere:
#
#   sh tools/crew_config_test.sh
#
# It needs jq, as session-finished does.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
config=$here/../.crew/config.yaml
skills=$here/../.agents/skills

command -v jq >/dev/null || {
	echo "jq is not on the PATH; session-finished needs it" >&2
	exit 1
}

root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
failures=0

fail() {
	echo "FAIL: $*"
	failures=$((failures + 1))
}

# definition NAME prints the definition of the shell action NAME under the
# config's top-level actions, its keys unindented: "script: |-" and the
# script for a definition given as a string, or the keys of a map.
definition() {
	awk -v key="  $1:" '
		/^[^ #]/ { if (on) exit; top = ($0 == "actions:"); next }
		top && $0 == key " |-" { on = 3; print "script: |-"; next }
		top && $0 == key { on = 5; next }
		on && /^    / { print substr($0, on); next }
		on && /^$/ { print ""; next }
		on { exit }
	' "$config"
}

# rule NAME prints the rule NAME under the config's rules, its keys
# unindented.
rule() {
	awk -v key="  $1:" '
		/^[^ #]/ { if (on) exit; top = ($0 == "rules:"); next }
		top && $0 == key { on = 1; next }
		on && /^    / { print substr($0, 5); next }
		on && /^$/ { print ""; next }
		on { exit }
	' "$config"
}

# block KEY prints the block under the line KEY of its input, unindented.
block() {
	awk -v key="$1" '
		$0 == key { on = 1; next }
		on && /^  / { print substr($0, 3); next }
		on && /^$/ { print ""; next }
		on { exit }
	'
}

# shell NAME prints the script of the shell action NAME.
shell() {
	definition "$1" | block "script: |-"
}

# sequence RULE prints the actions of the rule RULE in order, one a line:
# "session NAME" or "shell NAME", followed by " VERDICT=ROUTE" for each
# entry of its on.
sequence() {
	rule "$1" | block "actions:" | awk '
		function flush() {
			if (kind != "") print kind " " name on
			kind = ""; name = ""; on = ""; in_on = 0
		}
		/^- / {
			flush()
			rest = substr($0, 3)
			if (rest !~ /^[a-z_]+: /) { kind = "shell"; name = rest; sub(/:$/, "", name); next }
			kind = "session"; $0 = "  " rest
		}
		/^  name: / { name = substr($0, 9) }
		$0 == "  on:" { in_on = 1; next }
		in_on && /^    [^ ]/ { entry = substr($0, 5); sub(/: /, "=", entry); on = on " " entry; next }
		/^  [^ ]/ { in_on = 0 }
		END { flush() }
	'
}

# prompt RULE NAME prints the prompt of the session NAME of the rule RULE.
prompt() {
	rule "$1" | block "actions:" | awk -v name="$2" '
		function flush() { if (this == name) printf "%s", text; this = ""; text = ""; on = 0 }
		/^- / { flush(); $0 = "  " substr($0, 3) }
		$0 == "  name: " name { this = name; on = 0; next }
		$0 == "  prompt: |-" { on = 1; next }
		on && /^    / { text = text substr($0, 5) "\n"; next }
		on && /^$/ { text = text "\n"; next }
		{ on = 0 }
		END { flush() }
	'
}

# at NEEDLE TEXT prints the offset of NEEDLE's first occurrence in TEXT, or
# -1 when TEXT lacks it.
at() {
	case $2 in
	*"$1"*)
		before=${2%%"$1"*}
		echo "${#before}"
		;;
	*) echo -1 ;;
	esac
}

# has TEXT NEEDLE WHAT fails, naming WHAT, unless TEXT holds NEEDLE.
has() {
	case $1 in
	*"$2"*) ;;
	*) fail "$3 lacks $2" ;;
	esac
}

# The outcomes /cw-split-plan reports and the refine prompt acts on (#160),
# one per line.
# shellcheck disable=SC2016 # the backticks are the outcomes' own, not commands
split_outcomes='`not split`
`kept whole`
`split`
`earlier split did not finish`'

# The markers /cw-split-plan writes: part_marker, followed by the parent's
# reference and " -->", on each part, and record_marker on the split record.
part_marker='<!-- cw-split-plan: part of '
record_marker='<!-- cw-split-plan: split record -->'

prompt=$(prompt refinement refine)
[ -n "$prompt" ] || fail "the refinement rule has no refine session with a prompt"
split_skill=$(cat "$skills/cw-split-plan/SKILL.md")
rank_skill=$(cat "$skills/cw-rank-blockers/SKILL.md")

# The refine session splits a large plan before it finds blockers, finishes
# the split by labelling the parts and taking the parent out of crew, and
# the shell action after it fails a split that stopped before that (#160).
split=$(at '/cw-split-plan {{.Issue.Ref}}' "$prompt")
if [ "$split" -lt 0 ] || [ "$split" -gt "$(at 'dependencies/blocked_by' "$prompt")" ]; then
	fail "the refine prompt does not run /cw-split-plan before it reads dependencies"
fi
for want in '--add-label "crew:refinement:done"' '--remove-label "crew:refinement:in progress"' "$record_marker"; do
	has "$prompt" "$want" "the refine prompt"
done
want='session refine
shell split-finished'
got=$(sequence refinement)
[ "$got" = "$want" ] || fail "the refinement rule runs \"$got\", want \"$want\""
finished=$(shell split-finished)
for want in '"crew:refinement:in progress"' "${part_marker}\$CREW_ISSUE_REF -->" /sub_issues; do
	has "$finished" "$want" "split-finished"
done

# The refine prompt branches on the outcomes /cw-split-plan reports, so both
# name the same outcomes and markers.
while IFS= read -r outcome; do
	has "$split_skill" "$outcome" "/cw-split-plan"
	has "$prompt" "$outcome" "the refine prompt"
done <<EOF
$split_outcomes
EOF
has "$split_skill" "${part_marker}#" "/cw-split-plan"
has "$split_skill" "$record_marker" "/cw-split-plan"

# The refine prompt reads in full only the candidates /cw-rank-blockers
# shortlists for each issue it refines, after the split and before it
# records a dependency, and falls back to reading every candidate when the
# script fails (#166).
has "$rank_skill" "name: cw-rank-blockers" "the skill the refine prompt runs"
rank=$(at /cw-rank-blockers "$prompt")
record=$(at 'dependencies/blocked_by -F' "$prompt")
if [ "$split" -lt 0 ] || [ "$rank" -lt "$split" ] || [ "$record" -lt "$rank" ]; then
	fail "the refine prompt does not run /cw-rank-blockers between /cw-split-plan and recording"
fi
for want in '--json number,title,body,labels' unavailable; do
	has "$prompt" "$want" "the refine prompt's full read when the shortlist is unavailable"
done

# session-finished, the judge, runs against these stubs. curl counts its
# calls, keeps its arguments, the headers it read from stdin and the
# request it was given, then writes $STUB_DIR/body to its -o file and prints
# $STUB_CODE, as curl's -w '%{http_code}' would; 000 is no answer. sleep
# records how long the judge waited instead of waiting.
# development and fix judge their lfg session, send the judge's exit 3 to
# a needs-person route, then check the pull request (AE2 of #254).
judge=$(shell session-finished)
[ -n "$judge" ] || fail "the config has no session-finished shell action"
verdicts=$(definition session-finished | block "verdicts:")
[ "$verdicts" = "3: needs_person" ] || fail "session-finished's verdicts are \"$verdicts\", want \"3: needs_person\""
want='session lfg
shell session-finished needs_person=needs-person
shell pr-closes-issue'
for name in development fix; do
	got=$(sequence "$name")
	[ "$got" = "$want" ] || fail "the $name rule runs \"$got\", want \"$want\""
	rule "$name" | block "routes:" | grep -qx 'needs-person:' || fail "the $name rule has no needs-person route"
done
mkdir "$root/bin"
cat >"$root/bin/curl" <<'EOF'
#!/bin/sh
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
EOF
cat >"$root/bin/sleep" <<'EOF'
#!/bin/sh
echo "$1" >> "$STUB_DIR/slept"
EOF
chmod +x "$root/bin/curl" "$root/bin/sleep"

key=ts-test-key-4c1d
# shellcheck disable=SC2016 # the backticks are the prompt's own, not commands
judge_prompt='/compound-engineering:lfg #9

The pull request body must contain the line `Closes #9`.'

# run_judge KEY LAST CODE BODY runs the judge as the shell adapter would,
# with TYPESAFE_API_KEY set to KEY unless KEY is empty, the session's last
# message LAST, and TypeSafe answering BODY with the HTTP status CODE. It
# leaves the stubs' files in $dir, its exit in $exit, the last line it
# printed in $line, its calls to curl in $calls and its waits in $slept.
run_judge() {
	dir=$(mktemp -d "$root/run.XXXXXX")
	mkdir "$dir/work"
	printf '%s' "$4" >"$dir/body"
	printf '%s' "$judge_prompt" >"$dir/prompt"
	printf '%s' "$2" >"$dir/last-message"
	exit=0
	out=$(cd "$dir/work" && env -i PATH="$root/bin:$PATH" STUB_DIR="$dir" STUB_CODE="$3" \
		CREW_ACTION=lfg CREW_PROMPT_FILE="$dir/prompt" CREW_LAST_MESSAGE_FILE="$dir/last-message" \
		${1:+TYPESAFE_API_KEY="$1"} sh -c "$judge" 2>&1) || exit=$?
	line=$(printf '%s\n' "$out" | sed '/^[[:space:]]*$/d' | tail -n 1)
	calls=0
	[ ! -e "$dir/calls" ] || calls=$(grep -c call "$dir/calls")
	slept=
	[ ! -e "$dir/slept" ] || slept=$(tr '\n' ' ' <"$dir/slept" | sed 's/ $//')
}

# expect NAME EXIT LINE CALLS SLEPT fails the case NAME unless the last
# run_judge ended so.
expect() {
	if [ "$exit" != "$2" ] || [ "$line" != "$3" ] || [ "$calls" != "$4" ] || [ "$slept" != "$5" ]; then
		fail "$1: exit $exit, \"$line\" after $calls calls, slept \"$slept\"; want exit $2, \"$3\" after $4, slept \"$5\""
	fi
}

# answer A B is TypeSafe's answer giving each option of Choice a the
# probabilities A, and of Choice b B, both JSON objects.
answer() {
	printf '{"model":"jev-1.13.0","answers":{"a":{"type":"choice","probabilities":%s},"b":{"type":"choice","probabilities":%s}}}' "$1" "$2"
}

# both is answer with the same probabilities in both orders.
both() {
	answer "$1" "$1"
}

# p DONE UNFINISHED NEEDS_PERSON STOPPED is the probabilities of the four
# options.
p() {
	printf '{"done":%s,"unfinished":%s,"needs_person":%s,"stopped":%s}' "$1" "$2" "$3" "$4"
}

# R7, R8: the judge fails on unfinished or stopped, exits 3 when the session
# needs a person, and passes otherwise, echoing the outcome and its
# probability averaged over both orders.
verdict() {
	run_judge "$key" 'PR #20 is open.
Merging is yours.' 200 "$2"
	expect "$1" "$3" "$4" 1 ""
}
verdict "unfinished (AE1)" "$(both "$(p 0 1 0 0)")" 1 "unfinished (1.00)"
verdict "done (AE2)" "$(both "$(p 0.97 0.01 0.01 0.01)")" 0 "done (0.97)"
verdict "needs a person (AE4)" "$(both "$(p 0.03 0.01 0.95 0.01)")" 3 "needs a person (0.95)"
verdict "needs a person below its threshold" "$(both "$(p 0.4 0.05 0.5 0.05)")" 0 "done (0.40)"
verdict "stopped" "$(both "$(p 0.3 0.1 0 0.6)")" 1 "stopped (0.60)"
verdict "unfinished and stopped reach the threshold together" "$(both "$(p 0.4 0.3 0 0.3)")" 1 "unfinished (0.30)"
verdict "unfinished and stopped below the threshold" "$(both "$(p 0.5 0.25 0 0.25)")" 0 "done (0.50)"
verdict "the two orders are averaged" "$(answer "$(p 0.2 0.8 0 0)" "$(p 0.6 0.4 0 0)")" 1 "unfinished (0.60)"

# R9, R10: the judge passes, saying so, when it cannot judge, and fails
# when it has no key.
done_answer=$(both "$(p 1 0 0 0)")
run_judge "$key" Done. 429 '{}'
expect "rate limited on every try (AE5)" 0 "not judged: TypeSafe answered HTTP 429" 3 "2 4"
run_judge "$key" Done. 529 '{}'
expect "overloaded on every try" 0 "not judged: TypeSafe answered HTTP 529" 3 "2 4"
run_judge "$key" Done. 000 ''
expect "no answer" 0 "not judged: TypeSafe could not be reached" 3 "2 4"
run_judge "$key" Done. 401 '{"error":"bad key"}'
expect "refused" 0 "not judged: TypeSafe answered HTTP 401" 1 ""
run_judge "$key" Done. 200 '<html>'
expect "an answer that is not JSON" 0 "not judged: TypeSafe's answer could not be read" 1 ""
run_judge "$key" Done. 200 '{"answers":{}}'
expect "an answer without probabilities" 0 "not judged: TypeSafe's answer could not be read" 1 ""
run_judge "" Done. 200 "$done_answer"
expect "no key (AE6)" 1 "TYPESAFE_API_KEY is not set: the judge cannot ask Jev" 0 ""
run_judge "$key" "" 200 "$done_answer"
expect "an empty last message (AE7)" 0 "not judged: the session's last message is empty" 0 ""
run_judge "$key" "$(printf ' \n\t\n')" 200 "$done_answer"
expect "a blank last message" 0 "not judged: the session's last message is empty" 0 ""

# R7, R11: the judge asks jev-1.13.0 one Choice in two option orders, with
# the action, the prompt and the last message as they are, and keeps the
# key out of curl's arguments.
run_judge "$key" "The suite is still running in the background; I'll pick up when it reports back.
\"quoted\" and \`ticks\`" 200 "$done_answer"
jq -e --rawfile prompt "$dir/prompt" --rawfile last "$dir/last-message" '
	.model == "jev-1.13.0"
	and .state == {action: "lfg", prompt: $prompt, last_message: $last}
	and (.questions | keys_unsorted) == ["a", "b"]
	and .questions.a.type == "choice" and .questions.b.type == "choice"
	and (.questions.a.criteria | keys_unsorted) == ["done", "unfinished", "needs_person", "stopped"]
	and (.questions.b.criteria | keys_unsorted) == ["stopped", "needs_person", "unfinished", "done"]
' "$dir/request" >/dev/null || fail "the judge's request is not one Choice in two orders of jev-1.13.0 over the action, prompt and last message: $(cat "$dir/request")"
if grep -qF "$key" "$dir/args" || ! grep -qxF "Authorization: Bearer $key" "$dir/headers"; then
	fail "curl got the key in its arguments or not in its headers"
fi

if [ "$failures" -gt 0 ]; then
	echo "$failures failed"
	exit 1
fi
echo ok
