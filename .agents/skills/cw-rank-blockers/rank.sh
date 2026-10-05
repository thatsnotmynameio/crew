#!/bin/sh
# Ranks, for /cw-rank-blockers, the open issues most likely to block an issue
# and the open issues it most likely blocks, by asking Jev about each pair.
#
#   sh rank.sh ISSUE      (ISSUE is #N or N)
#
# The candidates are the open issues the logins in $CREW_CODE_OWNERS and
# $CREW_BOTS opened, as in step 4 of the refine prompt, without the issue
# itself, split parents (bodies holding the split record marker) and, when
# the issue is a part of a split, its parent and the other parts. For each
# candidate one request to TypeSafe holds only the two issues' titles and
# bodies and asks two yes/no questions, one per direction. The script prints
# the top 5 of each direction with Jev's probability, or, when it cannot
# judge every candidate, no list and one line on stderr naming the cause.
# It only reads: it never changes an issue, a label or a dependency.
#
# One request per pair, the first 3,000 characters of each body and the
# pinned model are what #149 measured (a ranking score of 0.933, against
# 0.583 with every candidate in one request); #153 holds the numbers.
set -eu

model=jev-1.13.0
body_limit=3000
concurrency=8
attempts=3
list_length=5
endpoint=https://api.typesafe.ai/v1/systemone
empty_body='(no description)'

usage() {
	echo "usage: sh rank.sh ISSUE" >&2
	exit 2
}

fail() {
	echo "rank.sh: $*" >&2
	exit 1
}

[ "$#" -eq 1 ] || usage
issue=${1#\#}
case $issue in
'' | *[!0-9]*) usage ;;
esac

[ -n "${TYPESAFE_API_KEY:-}" ] || fail "TYPESAFE_API_KEY is not set"
logins=$(printf '%s %s' "${CREW_CODE_OWNERS:-}" "${CREW_BOTS:-}" | tr -s ' ')
[ -n "${logins# }" ] || fail "CREW_CODE_OWNERS and CREW_BOTS are both empty: the candidates are the open issues their logins opened"
for command in gh jq curl; do
	command -v "$command" >/dev/null 2>&1 || fail "$command is not installed"
done

umask 077
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 1' HUP INT TERM

# one_line prints file $1 on one line, for an error message.
one_line() {
	tr '\n' ' ' <"$1" | sed 's/ *$//'
}

gh issue view "$issue" --json number,title,body >"$tmp/issue.json" 2>"$tmp/gh.err" ||
	fail "gh issue view $issue failed: $(one_line "$tmp/gh.err")"
for login in $logins; do
	gh issue list --state open --author "$login" --limit 500 --json number,title,body >>"$tmp/lists.json" 2>"$tmp/gh.err" ||
		fail "gh issue list --author $login failed: $(one_line "$tmp/gh.err")"
done

parent=$(jq -r '(.body // "") | capture("<!-- cw-split-plan: part of #(?<p>[0-9]+) -->").p' "$tmp/issue.json" | head -n 1)
jq -s --argjson issue "$issue" --arg parent "$parent" '
	add // [] | unique_by(.number) | map(select(
		.number != $issue
		and ((.body // "") | contains("<!-- cw-split-plan: split record -->") | not)
		and ($parent == "" or (
			.number != ($parent | tonumber)
			and ((.body // "") | contains("<!-- cw-split-plan: part of #" + $parent + " -->") | not)
		))
	))' "$tmp/lists.json" >"$tmp/candidates.json"
numbers=$(jq -r '.[].number' "$tmp/candidates.json")

# The key reaches curl only through this file, written by the shell's own
# printf, so it never appears in a process's arguments.
printf 'Authorization: Bearer %s\n' "$TYPESAFE_API_KEY" >"$tmp/auth"

# request N writes the request about candidate N.
request() {
	jq -n --slurpfile issue "$tmp/issue.json" --slurpfile candidates "$tmp/candidates.json" \
		--argjson n "$1" --arg model "$model" --argjson limit "$body_limit" --arg empty "$empty_body" '
		def text: (. // "") | if . == "" then $empty else .[:$limit] end;
		($candidates[0][] | select(.number == $n)) as $candidate |
		{
			model: $model,
			state: {
				issue: {title: $issue[0].title, body: ($issue[0].body | text)},
				candidate: {title: $candidate.title, body: ($candidate.body | text)}
			},
			questions: {
				issue_needs_candidate: {
					type: "noul",
					instructions: "Does the work `issue` asks for build on the change `candidate` asks for, so that `candidate` must be done before `issue`?",
					criteria: {
						true: "`issue` needs something `candidate` adds or changes: it cannot be built, or would be built differently, before `candidate` is done.",
						false: "`issue` can be built and merged without `candidate`; they are unrelated, or only touch the same area."
					}
				},
				candidate_needs_issue: {
					type: "noul",
					instructions: "Does the work `candidate` asks for build on the change `issue` asks for, so that `issue` must be done before `candidate`?",
					criteria: {
						true: "`candidate` needs something `issue` adds or changes: it cannot be built, or would be built differently, before `issue` is done.",
						false: "`candidate` can be built and merged without `issue`; they are unrelated, or only touch the same area."
					}
				}
			}
		}' >"$tmp/request.$1.json"
}

# judge N asks Jev about candidate N and writes result.N, or fail.N with
# the cause. 429, 529, other 5xx, timeouts and connection failures are
# retried with backoff; any other answer is final.
judge() {
	n=$1
	attempt=1
	while :; do
		code=0
		status=$(curl -sS --max-time 60 -o "$tmp/response.$n.json" -w '%{http_code}' \
			-H @"$tmp/auth" -H 'Content-Type: application/json' \
			--data @"$tmp/request.$n.json" "$endpoint" 2>/dev/null) || code=$?
		retry=no
		if [ "$code" -eq 28 ]; then
			cause=timeout retry=yes
		elif [ "$code" -ne 0 ]; then
			cause="connection failed" retry=yes
		elif [ "$status" = 200 ]; then
			if jq -c --argjson n "$n" '
				.answers.issue_needs_candidate.noul as $blocker |
				.answers.candidate_needs_issue.noul as $blocked |
				if ($blocker | type) == "number" and ($blocked | type) == "number"
				then {number: $n, blocker: $blocker, blocked: $blocked}
				else error("no noul") end' "$tmp/response.$n.json" >"$tmp/result.$n" 2>/dev/null; then
				return 0
			fi
			rm -f "$tmp/result.$n"
			cause="invalid response"
		else
			cause="HTTP $status"
			case $status in
			429 | 5??) retry=yes ;;
			esac
		fi
		if [ "$retry" = no ] || [ "$attempt" -ge "$attempts" ]; then
			if [ "$attempt" -eq 1 ]; then tries="1 attempt"; else tries="$attempt attempts"; fi
			echo "Jev failed for #$n: $cause after $tries" >"$tmp/fail.$n"
			return 0
		fi
		sleep "$attempt"
		attempt=$((attempt + 1))
	done
}

# first_failure prints the failure of the lowest-numbered candidate, if any.
first_failure() {
	for n in $numbers; do
		if [ -e "$tmp/fail.$n" ]; then
			cat "$tmp/fail.$n"
			return 0
		fi
	done
}

# Requests run in batches of $concurrency. After a failure no new batch
# starts, so an outage reports its cause without waiting on every candidate.
running=0
for n in $numbers; do
	request "$n"
	judge "$n" &
	running=$((running + 1))
	if [ "$running" -ge "$concurrency" ]; then
		wait
		running=0
		failure=$(first_failure)
		[ -z "$failure" ] || fail "$failure"
	fi
done
wait
failure=$(first_failure)
[ -z "$failure" ] || fail "$failure"
for n in $numbers; do
	[ -s "$tmp/result.$n" ] || fail "Jev failed for #$n: no result"
done

# Each list: the $list_length most likely candidates, highest probability
# first, ties by issue number.
for n in $numbers; do cat "$tmp/result.$n"; done |
	jq -rs --slurpfile candidates "$tmp/candidates.json" --arg issue "#$issue" \
		--arg model "$model" --argjson length "$list_length" '
	($candidates[0] | map({key: (.number | tostring), value: .title}) | from_entries) as $titles |
	(length | if . == 1 then "1 candidate" else "\(.) candidates" end) as $count |
	def two: (. * 100 | round) as $c | "\($c / 100 | floor).\($c % 100 | tostring | if length == 1 then "0" + . else . end)";
	def ranked(f): sort_by(-(f), .number) | .[:$length] |
		if length == 0 then "(none)"
		else to_entries | map("\(.key + 1). #\(.value.number) \(.value | f | two) \($titles[.value.number | tostring])") | join("\n") end;
	"Likely to block \($issue) (\($count), \($model)):\n\(ranked(.blocker))\n\nLikely blocked by \($issue) (\($count), \($model)):\n\(ranked(.blocked))"'
