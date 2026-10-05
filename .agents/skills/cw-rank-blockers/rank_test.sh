#!/bin/sh
# Tests rank.sh with stub gh and curl commands first on PATH, so it runs
# without GitHub or TypeSafe. Run it from anywhere:
#
#   sh .agents/skills/cw-rank-blockers/rank_test.sh
#
# RANK_SHELL picks the shell that runs rank.sh (default sh), for example
# RANK_SHELL=dash to check that it stays POSIX.
#
# Each case builds a fixture directory: issue.N.json for `gh issue view N`,
# list.LOGIN.json for `gh issue list --author LOGIN`, and jev.TITLE for the
# Jev answers about the candidate titled TITLE, one line per attempt (the
# last line repeats): "200 BLOCKER BLOCKED", "200 invalid", an HTTP status,
# "timeout" or "unreachable". The stubs log their arguments to gh.log and
# curl.log and keep every request body under requests/.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
script=$here/rank.sh
shell=${RANK_SHELL:-sh}
key=not-a-real-key

root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT
mkdir "$root/bin"

cat >"$root/bin/gh" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$FIXTURE/gh.log"
if [ -e "$FIXTURE/gh.fail" ]; then
	cat "$FIXTURE/gh.fail" >&2
	exit 1
fi
case "$1 $2" in
"issue view")
	cat "$FIXTURE/issue.$3.json"
	;;
"issue list")
	login=
	while [ "$#" -gt 0 ]; do
		[ "$1" = --author ] && login=$2
		shift
	done
	if [ -e "$FIXTURE/list.$login.json" ]; then cat "$FIXTURE/list.$login.json"; else echo '[]'; fi
	;;
*)
	echo "gh stub: unexpected command: $*" >&2
	exit 1
	;;
esac
EOF

cat >"$root/bin/curl" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$FIXTURE/curl.log"
out= data= auth= url=
while [ "$#" -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;;
	--data) data=${2#@}; shift ;;
	-H)
		case $2 in @*) auth=${2#@} ;; esac
		shift
		;;
	-*) ;;
	*) url=$1 ;;
	esac
	shift
done
# Like TypeSafe, answer 401 to a request without the key's header, and 404
# to any other URL, so every passing case proves both reach curl.
if [ -z "$auth" ] || [ "$(cat "$auth")" != "Authorization: Bearer $TYPESAFE_API_KEY" ]; then
	echo '{"detail":"unauthorized"}' >"$out"
	printf 401
	exit 0
fi
if [ "$url" != https://api.typesafe.ai/v1/systemone ]; then
	echo '{"detail":"not found"}' >"$out"
	printf 404
	exit 0
fi
title=$(jq -r .state.candidate.title "$data")
count=$(cat "$FIXTURE/count.$title" 2>/dev/null || echo 0)
count=$((count + 1))
echo "$count" >"$FIXTURE/count.$title"
mkdir -p "$FIXTURE/requests"
cp "$data" "$FIXTURE/requests/$title.$count.json"
lines=$(wc -l <"$FIXTURE/jev.$title")
if [ "$count" -le "$lines" ]; then
	answer=$(sed -n "${count}p" "$FIXTURE/jev.$title")
else
	answer=$(tail -n 1 "$FIXTURE/jev.$title")
fi
set -- $answer
case $1 in
timeout) exit 28 ;;
unreachable) exit 7 ;;
200)
	if [ "$2" = invalid ]; then
		echo '{"model":"jev-1.13.0","answers":{}}' >"$out"
	else
		printf '{"model":"jev-1.13.0","answers":{"issue_needs_candidate":{"type":"noul","noul":%s},"candidate_needs_issue":{"type":"noul","noul":%s}}}\n' "$2" "$3" >"$out"
	fi
	;;
*) echo '{"detail":"stub error"}' >"$out" ;;
esac
printf '%s' "$1"
EOF
chmod +x "$root/bin/gh" "$root/bin/curl"

passed=0
failed=0
ok() {
	passed=$((passed + 1))
}
bad() {
	failed=$((failed + 1))
	echo "FAIL [$case]: $*" >&2
}

# new_case NAME starts a fixture with the refined issue #100.
new_case() {
	case=$1
	FIXTURE=$root/$case
	export FIXTURE
	mkdir "$FIXTURE"
	issue 100 "the refined issue" "Refined body."
	CREW_CODE_OWNERS=owner
	CREW_BOTS="crew-clerk[bot]"
	TYPESAFE_API_KEY=$key
	export CREW_CODE_OWNERS CREW_BOTS TYPESAFE_API_KEY
}

# issue N TITLE BODY writes the issue gh issue view returns for N.
issue() {
	jq -n --argjson n "$1" --arg t "$2" --arg b "$3" '{number: $n, title: $t, body: $b}' >"$FIXTURE/issue.$1.json"
}

# list LOGIN [N TITLE BODY]... writes the open issues LOGIN opened.
list() {
	login=$1
	shift
	items='[]'
	while [ "$#" -gt 0 ]; do
		items=$(printf '%s' "$items" | jq --argjson n "$1" --arg t "$2" --arg b "$3" '. + [{number: $n, title: $t, body: $b}]')
		shift 3
	done
	printf '%s\n' "$items" >"$FIXTURE/list.$login.json"
}

# jev TITLE ANSWER... sets the Jev answers about candidate TITLE.
jev() {
	title=$1
	shift
	printf '%s\n' "$@" >"$FIXTURE/jev.$title"
}

run() {
	set +e
	PATH=$root/bin:$PATH "$shell" "$script" "$@" >"$FIXTURE/out" 2>"$FIXTURE/err"
	status=$?
	set -e
}

expect_status() {
	if [ "$status" -eq "$1" ]; then ok; else bad "exit status $status, want $1; stderr: $(cat "$FIXTURE/err")"; fi
}

expect_out() {
	if [ "$(cat "$FIXTURE/out")" = "$1" ]; then
		ok
	else
		bad "stdout:
$(cat "$FIXTURE/out")
want:
$1"
	fi
}

expect_no_out() {
	if [ -s "$FIXTURE/out" ]; then bad "stdout not empty: $(cat "$FIXTURE/out")"; else ok; fi
}

expect_err() {
	if grep -qF -- "$1" "$FIXTURE/err"; then ok; else bad "stderr does not name '$1': $(cat "$FIXTURE/err")"; fi
}

# calls TITLE prints how many requests Jev got about candidate TITLE.
calls() {
	cat "$FIXTURE/count.$1" 2>/dev/null || echo 0
}

expect_judged() {
	for title in "$@"; do
		if [ "$(calls "$title")" -ge 1 ]; then ok; else bad "$title was never sent to Jev"; fi
	done
}

expect_not_judged() {
	for title in "$@"; do
		if [ "$(calls "$title")" -eq 0 ]; then ok; else bad "$title was sent to Jev"; fi
	done
}

new_case three-candidates-below-0.2
list owner 100 "the refined issue" "Refined body." 11 "t11" "Body 11." 12 "t12" "Body 12."
list "crew-clerk[bot]" 13 "t13" "Body 13."
jev t11 "200 0.15 0.05"
jev t12 "200 0.05 0.19"
jev t13 "200 0.12 0.10"
run 100
expect_status 0
expect_out "Likely to block #100 (3 candidates, jev-1.13.0):
1. #11 0.15 t11
2. #13 0.12 t13
3. #12 0.05 t12

Likely blocked by #100 (3 candidates, jev-1.13.0):
1. #12 0.19 t12
2. #13 0.10 t13
3. #11 0.05 t11"
expect_not_judged "the refined issue"

new_case seven-candidates
list owner 21 t21 b 22 t22 b 23 t23 b 24 t24 b 25 t25 b 26 t26 b 27 t27 b
jev t21 "200 0.9 0.1"
jev t22 "200 0.8 0.2"
jev t23 "200 0.7 0.3"
jev t24 "200 0.6 0.4"
jev t25 "200 0.5 0.5"
jev t26 "200 0.4 0.6"
jev t27 "200 0.3 0.7"
run "#100"
expect_status 0
expect_out "Likely to block #100 (7 candidates, jev-1.13.0):
1. #21 0.90 t21
2. #22 0.80 t22
3. #23 0.70 t23
4. #24 0.60 t24
5. #25 0.50 t25

Likely blocked by #100 (7 candidates, jev-1.13.0):
1. #27 0.70 t27
2. #26 0.60 t26
3. #25 0.50 t25
4. #24 0.40 t24
5. #23 0.30 t23"

new_case ties-and-rounding
list owner 32 t32 b 31 t31 b
jev t31 "200 0.5 1"
jev t32 "200 0.5 0.004"
run 100
expect_status 0
expect_out "Likely to block #100 (2 candidates, jev-1.13.0):
1. #31 0.50 t31
2. #32 0.50 t32

Likely blocked by #100 (2 candidates, jev-1.13.0):
1. #31 1.00 t31
2. #32 0.00 t32"

new_case split-markers
issue 100 "the refined issue" "Part body.
<!-- cw-split-plan: part of #40 -->"
list owner 40 parent "Plan.
## Split
<!-- cw-split-plan: split record -->" 41 sibling "Sibling.
<!-- cw-split-plan: part of #40 -->" 50 "other parent" "Other plan.
<!-- cw-split-plan: split record -->" 51 "other part" "Part.
<!-- cw-split-plan: part of #50 -->" 52 "part of #4" "Part.
<!-- cw-split-plan: part of #4 -->"
list "crew-clerk[bot]" 51 "other part" "Part.
<!-- cw-split-plan: part of #50 -->"
jev "other part" "200 0.2 0.1"
jev "part of #4" "200 0.1 0.2"
run 100
expect_status 0
expect_not_judged parent sibling "other parent"
expect_judged "other part" "part of #4"
if [ "$(calls "other part")" -eq 1 ]; then ok; else bad "the issue both logins list was judged $(calls "other part") times"; fi
expect_out "Likely to block #100 (2 candidates, jev-1.13.0):
1. #51 0.20 other part
2. #52 0.10 part of #4

Likely blocked by #100 (2 candidates, jev-1.13.0):
1. #52 0.20 part of #4
2. #51 0.10 other part"

new_case request-shape
long=$(printf 'x%.0s' $(seq 1 3100))
odd='odd "q" back\slash %s	tab'
list owner 61 "long one" "$long" 62 "empty one" "" 63 "$odd" "Body with \\n and %d."
jev "long one" "200 0.3 0.3"
jev "empty one" "200 0.2 0.2"
jev "$odd" "200 0.1 0.1"
run 100
expect_status 0
request=$FIXTURE/requests/long\ one.1.json
if jq -e --arg model jev-1.13.0 '
	.model == $model
	and (keys == ["model", "questions", "state"])
	and (.state | keys == ["candidate", "issue"])
	and .state.issue == {title: "the refined issue", body: "Refined body."}
	and (.state.candidate | keys == ["body", "title"])
	and .state.candidate.title == "long one"
	and (.state.candidate.body | length == 3000)
	and (.questions | keys == ["candidate_needs_issue", "issue_needs_candidate"])
	and ([.questions[].type] == ["noul", "noul"])
' "$request" >/dev/null; then ok; else bad "unexpected request: $(cat "$request")"; fi
if jq -e '.state.candidate.body | length > 0' "$FIXTURE/requests/empty one.1.json" >/dev/null; then ok; else bad "an empty body was sent empty"; fi
if jq -e --arg t "$odd" '.state.candidate == {title: $t, body: "Body with \\n and %d."}' "$FIXTURE/requests/$odd.1.json" >/dev/null; then ok; else bad "quotes, backslashes, tabs or % changed on the way to Jev"; fi

new_case no-candidate
list owner 100 "the refined issue" "Refined body."
run 100
expect_status 0
expect_out "Likely to block #100 (0 candidates, jev-1.13.0):
(none)

Likely blocked by #100 (0 candidates, jev-1.13.0):
(none)"
if [ -e "$FIXTURE/curl.log" ]; then bad "curl was called without candidates"; else ok; fi

new_case no-key
list owner 11 t11 b
jev t11 "200 0.1 0.1"
TYPESAFE_API_KEY=
run 100
expect_status 1
expect_no_out
expect_err TYPESAFE_API_KEY
if [ -e "$FIXTURE/gh.log" ] || [ -e "$FIXTURE/curl.log" ]; then bad "gh or curl was called without a key"; else ok; fi

new_case no-logins
CREW_CODE_OWNERS=
CREW_BOTS=" "
run 100
expect_status 1
expect_no_out
expect_err CREW_CODE_OWNERS
expect_err CREW_BOTS

new_case gh-fails
echo "HTTP 502: Bad Gateway (https://api.github.com/graphql)" >"$FIXTURE/gh.fail"
run 100
expect_status 1
expect_no_out
expect_err "HTTP 502: Bad Gateway"

new_case unauthorized
list owner 11 t11 b 12 t12 b
jev t11 "200 0.1 0.1"
jev t12 401
run 100
expect_status 1
expect_no_out
expect_err "#12"
expect_err "HTTP 401"
if [ "$(calls t12)" -eq 1 ]; then ok; else bad "a 401 was retried: $(calls t12) requests"; fi

new_case overloaded-then-ok
list owner 11 t11 b
jev t11 529 529 "200 0.4 0.6"
run 100
expect_status 0
expect_out "Likely to block #100 (1 candidate, jev-1.13.0):
1. #11 0.40 t11

Likely blocked by #100 (1 candidate, jev-1.13.0):
1. #11 0.60 t11"

new_case overloaded-always
list owner 11 t11 b
jev t11 529
run 100
expect_status 1
expect_no_out
expect_err "#11"
expect_err "HTTP 529"
expect_err "3 attempts"
if [ "$(calls t11)" -eq 3 ]; then ok; else bad "want 3 attempts, got $(calls t11)"; fi

new_case other-retried-statuses
list owner 11 t11 b 12 t12 b
jev t11 503 "200 0.3 0.1"
jev t12 429 "200 0.2 0.4"
run 100
expect_status 0
if [ "$(calls t11)" -eq 2 ] && [ "$(calls t12)" -eq 2 ]; then ok; else bad "503 and 429 were not retried once: $(calls t11), $(calls t12)"; fi

new_case unreachable-always
list owner 11 t11 b
jev t11 unreachable
run 100
expect_status 1
expect_no_out
expect_err "connection failed"
expect_err "3 attempts"

new_case parent-without-record
issue 100 "the refined issue" "Part body.
<!-- cw-split-plan: part of #40 -->"
list owner 40 parent "Plan whose split did not finish." 11 t11 b
jev t11 "200 0.1 0.1"
run 100
expect_status 0
expect_not_judged parent
expect_judged t11

new_case invalid-response
list owner 11 t11 b
jev t11 "200 invalid"
run 100
expect_status 1
expect_no_out
expect_err "invalid response"

new_case outage-stops-early
list owner 1 c1 b 2 c2 b 3 c3 b 4 c4 b 5 c5 b 6 c6 b 7 c7 b 8 c8 b 9 c9 b 10 c10 b
for n in 1 2 3 4 5 6 7 8 9 10; do jev "c$n" timeout; done
run 100
expect_status 1
expect_no_out
expect_err "#1:"
expect_err timeout
expect_not_judged c9 c10

new_case key-stays-secret
list owner 11 t11 b
jev t11 "200 0.1 0.1"
run 100
expect_status 0
if grep -rqF -- "$key" "$FIXTURE"; then bad "the key appears in $(grep -rlF -- "$key" "$FIXTURE")"; else ok; fi

new_case usage
run
expect_status 2
expect_err usage
run 12x
expect_status 2

echo "rank_test.sh: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
