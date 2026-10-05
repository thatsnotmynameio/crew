#!/bin/sh
# Tests backtest.sh with stub gh and curl commands first on PATH, so it runs
# without GitHub or TypeSafe. The real rank.sh runs throughout, behind the
# backtest's own stand-in gh. Run it from anywhere:
#
#   sh .agents/skills/cw-rank-blockers/backtest_test.sh
#
# BACKTEST_SHELL picks the shell that runs backtest.sh (default sh), for
# example BACKTEST_SHELL=dash to check that it stays POSIX.
#
# Each case builds the repository's history in nodes.json with the helpers
# below; the gh stub answers `gh api graphql` with it as one page. Issue N
# is titled tN. The curl stub answers Jev from p.ISSUE.CANDIDATE (titles),
# else d.CANDIDATE, else "0.01 0.01": "BLOCKER BLOCKED" probabilities, or
# "fail STATUS". It logs each pair it judged to judged and keeps every
# request body under requests/.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
script=$here/backtest.sh
shell=${BACKTEST_SHELL:-sh}
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
"api graphql")
	# The paginated call lists the issue numbers; every other call looks
	# issues up by number, under the aliases the query gives them.
	case " $* " in
	*" --paginate "*)
		jq '{data: {repository: {nameWithOwner: "owner/repo", issues: {pageInfo: {hasNextPage: false, endCursor: null}, nodes: map({number})}}}}' "$FIXTURE/nodes.json"
		;;
	*)
		numbers=$(printf '%s' "$*" | grep -o 'issue(number: [0-9]*)' | tr -cd '0-9\n' | paste -sd, -)
		jq --argjson want "[$numbers]" '{data: {repository: (map(select(.number as $n | $want | index($n))) | map({key: "i\(.number)", value: .}) | from_entries)}}' "$FIXTURE/nodes.json"
		;;
	esac
	;;
*)
	echo "gh stub: unexpected command: $*" >&2
	exit 1
	;;
esac
EOF

cat >"$root/bin/curl" <<'EOF'
#!/bin/sh
out= data=
while [ "$#" -gt 0 ]; do
	case $1 in
	-o) out=$2; shift ;;
	--data) data=${2#@}; shift ;;
	-H) shift ;;
	esac
	shift
done
issue=$(jq -r .state.issue.title "$data")
candidate=$(jq -r .state.candidate.title "$data")
mkdir -p "$FIXTURE/requests"
cp "$data" "$FIXTURE/requests/$issue.$candidate.json"
printf '%s %s\n' "$issue" "$candidate" >>"$FIXTURE/judged"
if [ -e "$FIXTURE/p.$issue.$candidate" ]; then
	answer=$(cat "$FIXTURE/p.$issue.$candidate")
elif [ -e "$FIXTURE/d.$candidate" ]; then
	answer=$(cat "$FIXTURE/d.$candidate")
else
	answer="0.01 0.01"
fi
set -- $answer
if [ "$1" = fail ]; then
	echo '{"detail":"stub error"}' >"$out"
	printf '%s' "$2"
	exit 0
fi
printf '{"model":"jev-1.13.0","answers":{"issue_needs_candidate":{"type":"noul","noul":%s},"candidate_needs_issue":{"type":"noul","noul":%s}}}\n' "$1" "$2" >"$out"
printf 200
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

new_case() {
	case=$1
	FIXTURE=$root/$case
	export FIXTURE
	mkdir "$FIXTURE"
	echo '[]' >"$FIXTURE/nodes.json"
	CREW_CODE_OWNERS=owner
	CREW_BOTS="crew-product-manager[bot]"
	TYPESAFE_API_KEY=$key
	export CREW_CODE_OWNERS CREW_BOTS TYPESAFE_API_KEY
}

# save replaces nodes.json with the nodes.next the helper before it wrote.
save() {
	mv "$FIXTURE/nodes.next" "$FIXTURE/nodes.json"
}

# issue N CREATED [LOGIN [TYPE]] adds issue N, titled tN, body "Body N.".
issue() {
	jq --argjson n "$1" --arg at "$2" --arg login "${3:-owner}" --arg type "${4:-User}" \
		'. + [{number: $n, title: "t\($n)", body: "Body \($n).", createdAt: $at, closedAt: null,
		author: {login: $login, __typename: $type},
		userContentEdits: {totalCount: 0, nodes: []}, timelineItems: {totalCount: 0, nodes: []}}]' \
		"$FIXTURE/nodes.json" >"$FIXTURE/nodes.next"
	save
}

# body N TEXT sets issue N's current body.
body() {
	jq --argjson n "$1" --arg b "$2" 'map(if .number == $n then .body = $b else . end)' "$FIXTURE/nodes.json" >"$FIXTURE/nodes.next"
	save
}

# edit N EDITED_AT TEXT records an edit whose body after it is TEXT. Its
# createdAt is always later, as GitHub's is on the original body's entry.
edit() {
	jq --argjson n "$1" --arg at "$2" --arg b "$3" \
		'map(if .number == $n then .userContentEdits.nodes = ([{editedAt: $at, createdAt: "2030-01-01T00:00:00Z", diff: $b}] + .userContentEdits.nodes | sort_by(.editedAt) | reverse) | .userContentEdits.totalCount += 1 else . end)' \
		"$FIXTURE/nodes.json" >"$FIXTURE/nodes.next"
	save
}

# event N ITEM adds a timeline item (JSON) to issue N.
event() {
	jq --argjson n "$1" --argjson item "$2" \
		'map(if .number == $n then .timelineItems.nodes += [$item] | .timelineItems.totalCount += 1 else . end)' \
		"$FIXTURE/nodes.json" >"$FIXTURE/nodes.next"
	save
}

closed() {
	event "$1" "$(jq -n --arg at "$2" '{__typename: "ClosedEvent", createdAt: $at}')"
	jq --argjson n "$1" --arg at "$2" 'map(if .number == $n then .closedAt = $at else . end)' "$FIXTURE/nodes.json" >"$FIXTURE/nodes.next"
	save
}

reopened() {
	event "$1" "$(jq -n --arg at "$2" '{__typename: "ReopenedEvent", createdAt: $at}')"
	jq --argjson n "$1" 'map(if .number == $n then .closedAt = null else . end)' "$FIXTURE/nodes.json" >"$FIXTURE/nodes.next"
	save
}

# label N AT NAME and unlabel N AT NAME add and remove a label.
label() {
	event "$1" "$(jq -n --arg at "$2" --arg name "$3" '{__typename: "LabeledEvent", createdAt: $at, label: {name: $name}}')"
}

unlabel() {
	event "$1" "$(jq -n --arg at "$2" --arg name "$3" '{__typename: "UnlabeledEvent", createdAt: $at, label: {name: $name}}')"
}

# link BLOCKED BLOCKER AT [ACTOR [ACTOR_TYPE [REPO]]] records BLOCKED blocked_by BLOCKER.
link() {
	event "$1" "$(jq -n --argjson m "$2" --arg at "$3" --arg actor "${4:-owner}" --arg type "${5:-User}" --arg repo "${6:-owner/repo}" \
		'{__typename: "BlockedByAddedEvent", createdAt: $at, actor: {login: $actor, __typename: $type}, blockingIssue: {number: $m, repository: {nameWithOwner: $repo}}}')"
}

unlink() {
	event "$1" "$(jq -n --argjson m "$2" --arg at "$3" \
		'{__typename: "BlockedByRemovedEvent", createdAt: $at, actor: {login: "owner", __typename: "User"}, blockingIssue: {number: $m, repository: {nameWithOwner: "owner/repo"}}}')"
}

# p ISSUE CANDIDATE BLOCKER BLOCKED sets Jev's answers for one pair (issue
# numbers); d CANDIDATE BLOCKER BLOCKED for every issue asking about it.
p() {
	echo "$3 $4" >"$FIXTURE/p.t$1.t$2"
}

d() {
	echo "$2 $3" >"$FIXTURE/d.t$1"
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

expect_line() {
	if grep -qxF -- "$1" "$FIXTURE/out"; then
		ok
	else
		bad "stdout has no line '$1':
$(cat "$FIXTURE/out")"
	fi
}

expect_no_line() {
	if grep -qF -- "$1" "$FIXTURE/out"; then bad "stdout holds '$1'"; else ok; fi
}

expect_no_out() {
	if [ -s "$FIXTURE/out" ]; then bad "stdout not empty: $(cat "$FIXTURE/out")"; else ok; fi
}

expect_err() {
	if grep -qF -- "$1" "$FIXTURE/err"; then ok; else bad "stderr does not name '$1': $(cat "$FIXTURE/err")"; fi
}

# judged ISSUE CANDIDATE tells whether Jev was asked about the pair.
judged() {
	grep -qxF "t$1 t$2" "$FIXTURE/judged" 2>/dev/null
}

expect_judged() {
	issue=$1
	shift
	for c in "$@"; do
		if judged "$issue" "$c"; then ok; else bad "#$c was never judged for #$issue"; fi
	done
}

expect_not_judged() {
	issue=$1
	shift
	for c in "$@"; do
		if judged "$issue" "$c"; then bad "#$c was judged for #$issue"; else ok; fi
	done
}

# chain LINKS FOUND records LINKS hand-made links k blocked_by k+1, of which
# the first FOUND rank first and the rest below five fillers.
chain() {
	n=1
	while [ "$n" -le $(($1 + 1)) ]; do
		issue "$n" 2026-10-01T00:00:00Z
		n=$((n + 1))
	done
	for f in 51 52 53 54 55; do
		issue "$f" 2026-10-01T00:00:00Z
		d "$f" 0.5 0.5
	done
	n=1
	while [ "$n" -le "$1" ]; do
		link "$n" $((n + 1)) "2026-10-02T00:00:$(printf '%02d' "$n")Z"
		if [ "$n" -le "$2" ]; then p "$n" $((n + 1)) 0.9 0.01; else p "$n" $((n + 1)) 0.001 0.01; fi
		n=$((n + 1))
	done
}

new_case gate-passes
for n in 1 2 3 4 11 12 13 14; do issue "$n" 2026-10-01T00:00:00Z; done
for f in 21 22 23 24 25; do issue "$f" 2026-10-01T00:00:00Z; done
d 21 0.8 0.8
d 22 0.7 0.7
d 23 0.6 0.6
d 24 0.5 0.5
d 25 0.4 0.4
link 1 11 2026-10-02T10:00:00Z
link 2 12 2026-10-02T10:00:01Z
link 3 13 2026-10-02T10:00:02Z
link 4 14 2026-10-02T10:00:03Z
p 1 11 0.9 0.01
p 2 12 0.65 0.01
p 3 13 0.45 0.01
p 4 14 0.35 0.01
run
expect_status 0
expect_line "**Gate passed:** 3 of 4 links found (75.0%); the gate needs 70%."
expect_line "| #1 | #11 | 2026-10-02 10:00 | owner | #1 | 1 | not in top 5 |"
expect_line "| #2 | #12 | 2026-10-02 10:00 | owner | #2 | 3 | not in top 5 |"
expect_line "| #3 | #13 | 2026-10-02 10:00 | owner | #3 | 5 | not in top 5 |"
expect_line "| #4 | #14 | 2026-10-02 10:00 | owner | #4 | not in top 5 | not in top 5 |"
expect_line "- #4 blocked by #14, refined side #4: #21 0.80, #22 0.70, #23 0.60, #24 0.50, #25 0.40"
expect_no_line "- #1 blocked by #11"

new_case gate-fails-at-62.5
chain 16 10
run
expect_status 0
expect_line "**Gate failed:** 10 of 16 links found (62.5%); the gate needs 70%."
for n in 11 12 13 14 15 16; do expect_line "- #$n blocked by #$((n + 1)), refined side #$n: #51 0.50, #52 0.50, #53 0.50, #54 0.50, #55 0.50"; done
if [ "$(grep -c '^- #[0-9]* blocked by' "$FIXTURE/out")" -eq 6 ]; then ok; else bad "want 6 misses listed"; fi

new_case gate-passes-at-exactly-70
chain 10 7
run
expect_status 0
expect_line "**Gate passed:** 7 of 10 links found (70.0%); the gate needs 70%."

new_case refined-side-is-the-blocker
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z
for f in 51 52 53 54 55 56 57; do issue "$f" 2026-10-01T00:00:00Z; done
for f in 51 52 53; do d "$f" 0.5 0.6; done
for f in 54 55 56 57; do d "$f" 0.5 0.1; done
label 2 2026-10-02T09:59:00Z "crew:refinement:in progress"
link 1 2 2026-10-02T10:00:00Z crew-product-manager Bot
unlabel 2 2026-10-02T10:01:00Z "crew:refinement:in progress"
label 1 2026-10-03T09:00:00Z "crew:refinement:in progress"
p 1 2 0.1 0.01
p 2 1 0.01 0.45
run
expect_status 0
expect_line "| #1 | #2 | 2026-10-02 10:00 | crew-product-manager | #2 | 4 | not in top 5 |"
expect_line "**Gate passed:** 1 of 1 links found (100.0%); the gate needs 70%."

new_case hand-made-link-takes-the-blocked-side
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z
for f in 51 52 53 54 55 56 57; do issue "$f" 2026-10-01T00:00:00Z; done
for f in 51 52 53; do d "$f" 0.5 0.6; done
for f in 54 55 56 57; do d "$f" 0.5 0.1; done
label 2 2026-10-02T09:59:00Z "crew:refinement:in progress"
link 1 2 2026-10-02T10:00:00Z
p 1 2 0.1 0.01
p 2 1 0.01 0.45
run
expect_status 0
expect_line "| #1 | #2 | 2026-10-02 10:00 | owner | #1 | not in top 5 | 4 |"
expect_line "**Gate failed:** 0 of 1 links found (0.0%); the gate needs 70%."

new_case open-at-the-moment
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z
issue 3 2026-10-03T00:00:00Z
issue 4 2026-10-01T00:00:00Z
closed 4 2026-10-01T12:00:00Z
issue 5 2026-10-01T00:00:00Z
closed 5 2026-10-02T12:00:00Z
issue 6 2026-10-01T00:00:00Z
closed 6 2026-10-01T06:00:00Z
reopened 6 2026-10-01T07:00:00Z
link 1 2 2026-10-02T10:00:00Z
run
expect_status 0
expect_judged 1 2 5 6
expect_not_judged 1 3 4

new_case bodies-as-they-read
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z
issue 3 2026-10-01T00:00:00Z
edit 1 2026-10-01T00:00:00Z "Refined issue, first body."
edit 1 2026-10-03T00:00:00Z "Refined issue, rewritten later."
body 1 "Refined issue, rewritten later."
edit 3 2026-10-01T00:00:00Z "Candidate, first body."
edit 3 2026-10-01T05:00:00Z "Candidate, second body."
edit 3 2026-10-04T00:00:00Z "Candidate, rewritten later."
body 3 "Candidate, rewritten later."
body 2 "Never edited."
link 1 2 2026-10-02T10:00:00Z
run
expect_status 0
if jq -e '.state.issue.body == "Refined issue, first body." and .state.candidate.body == "Never edited."' "$FIXTURE/requests/t1.t2.json" >/dev/null; then ok; else bad "unexpected bodies: $(cat "$FIXTURE/requests/t1.t2.json")"; fi
if jq -e '.state.candidate.body == "Candidate, second body."' "$FIXTURE/requests/t1.t3.json" >/dev/null; then ok; else bad "unexpected body: $(cat "$FIXTURE/requests/t1.t3.json")"; fi

new_case authors
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z crew-product-manager Bot
issue 3 2026-10-01T00:00:00Z stranger
link 1 2 2026-10-02T10:00:00Z
run
expect_status 0
expect_judged 1 2
expect_not_judged 1 3
expect_line "**Gate passed:** 1 of 1 links found (100.0%); the gate needs 70%."

new_case exclusions
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z
issue 60 2026-10-01T00:00:00Z
body 60 "Plan.
<!-- cw-split-plan: split record -->"
issue 61 2026-10-01T00:00:00Z
body 61 "Part one.
<!-- cw-split-plan: part of #60 -->"
issue 62 2026-10-01T00:00:00Z
body 62 "Part two.
<!-- cw-split-plan: part of #60 -->"
issue 7 2026-10-01T00:00:00Z
closed 7 2026-10-01T12:00:00Z
issue 8 2026-10-01T00:00:00Z stranger
issue 9 2026-10-01T00:00:00Z
link 62 61 2026-10-02T10:00:00Z crew-product-manager Bot
link 1 7 2026-10-02T10:00:00Z
link 1 8 2026-10-02T10:00:00Z
link 1 5 2026-10-02T10:00:00Z owner User other/repo
link 2 9 2026-10-02T10:00:00Z
unlink 2 9 2026-10-02T11:00:00Z
link 2 1 2026-10-02T12:00:00Z
run
expect_status 0
expect_line "- #62 blocked by #61: both are parts of the split of #60"
expect_line "- #1 blocked by #7: #7 was not open then"
expect_line "- #1 blocked by #8: #8 was opened by stranger, whose issues crew does not take"
expect_line "- #1 blocked by other/repo#5: the blocking issue is in another repository"
expect_no_line "#9"
expect_line "**Gate passed:** 1 of 1 links found (100.0%); the gate needs 70%."
expect_line "6 links recorded: 1 measured, 4 excluded, 1 removed since."

new_case no-body-text-in-report
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z
body 1 "Distinctive phrase alpha."
body 2 "Distinctive phrase beta."
link 1 2 2026-10-02T10:00:00Z
run
expect_status 0
if grep -qF "Distinctive phrase" "$FIXTURE/out"; then bad "the report holds body text"; else ok; fi

new_case odd-bodies-reach-jev
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z
# shellcheck disable=SC2016 # $(not run) is test data that must reach Jev unexpanded
odd=$(printf 'carriage\rreturn, `backticks`, $(not run) and \\033[31m')
body 2 "$odd"
link 1 2 2026-10-02T10:00:00Z
run
expect_status 0
if jq -e --arg b "$odd" '.state.candidate.body == $b' "$FIXTURE/requests/t1.t2.json" >/dev/null; then ok; else bad "the body changed on its way to Jev"; fi

new_case jev-fails
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z
issue 3 2026-10-01T00:00:00Z
link 1 2 2026-10-02T10:00:00Z
echo "fail 401" >"$FIXTURE/p.t1.t3"
run
expect_status 1
expect_no_out
expect_err "backtest.sh: rank.sh #1 failed for the link #1 blocked by #2: rank.sh: Jev failed for #3: HTTP 401 after 1 attempt"

new_case no-key
TYPESAFE_API_KEY=
run
expect_status 1
expect_no_out
expect_err TYPESAFE_API_KEY
if [ -e "$FIXTURE/gh.log" ]; then bad "gh was called without a key"; else ok; fi

new_case no-logins
CREW_CODE_OWNERS=
CREW_BOTS=" "
run
expect_status 1
expect_err CREW_CODE_OWNERS

new_case gh-fails
echo "HTTP 502: Bad Gateway" >"$FIXTURE/gh.fail"
run
expect_status 1
expect_no_out
expect_err "HTTP 502: Bad Gateway"

new_case truncated-page
issue 1 2026-10-01T00:00:00Z
issue 2 2026-10-01T00:00:00Z
link 1 2 2026-10-02T10:00:00Z
jq 'map(if .number == 2 then .userContentEdits.totalCount = 101 else . end)' "$FIXTURE/nodes.json" >"$FIXTURE/nodes.next"
save
run
expect_status 1
expect_no_out
expect_err "#2"
expect_err "more than"

new_case nothing-to-measure
issue 1 2026-10-01T00:00:00Z
run
expect_status 1
expect_no_out
expect_err "no link"

new_case usage
run extra
expect_status 2
expect_err usage

echo "backtest_test.sh: $passed passed, $failed failed"
[ "$failed" -eq 0 ]
