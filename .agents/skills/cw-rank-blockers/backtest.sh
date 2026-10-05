#!/bin/sh
# Measures, for #166's adoption gate, how often rank.sh puts a real
# dependency in its top 5, by replaying it against every blocked_by link
# GitHub records in this repository.
#
#   sh backtest.sh      (from the repository root, with rank.sh's environment)
#
# Each link is replayed at the moment it was recorded (T), from the side
# whose refinement recorded it: the issue that carried the refinement
# running label at T, or the blocked issue for a link recorded by hand. A
# stand-in gh first on PATH answers rank.sh's two reads from the
# repository as it was at T: the issues open then, with their bodies as
# they read then (from GitHub's edit history). curl still reaches Jev, so
# the gate measures the script that ships, not a copy. The other side is
# replayed too and reported, but does not gate.
#
# It prints a markdown report made only of computed data (issue numbers,
# ranks, Jev's probabilities), ending with the gate line: at least 70% of
# the measured links found. It exits 0 whenever it measured, 1 with one
# line on stderr when it could not judge every link, 2 on a usage error.
# It only reads: it never changes an issue, a label or a dependency.
set -eu

gate_percent=70
list_length=5
running_label="crew:refinement:in progress"
split_record="<!-- cw-split-plan: split record -->"

here=$(cd "$(dirname "$0")" && pwd)

fail() {
	echo "backtest.sh: $*" >&2
	exit 1
}

if [ "$#" -ne 0 ]; then
	echo "usage: sh backtest.sh" >&2
	exit 2
fi

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

# The issue list comes from the paginated issues connection, but each
# issue's history from issue(number:) lookups: on 2026-10-05 the
# connection's nested timelineItems were stale (none for #133, #135 and
# #139, which issue(number:) returned 16, 18 and 2 of).
# shellcheck disable=SC2016 # $owner, $name and $endCursor are GraphQL variables
list_query='query($owner: String!, $name: String!, $endCursor: String) {
  repository(owner: $owner, name: $name) {
    nameWithOwner
    issues(first: 100, after: $endCursor, states: [OPEN, CLOSED]) { pageInfo { hasNextPage endCursor } nodes { number } }
  }
}'
history_fields='fragment history on Issue {
  number title body createdAt closedAt
  author { login __typename }
  userContentEdits(first: 100) { totalCount nodes { editedAt diff } }
  timelineItems(first: 100, itemTypes: [BLOCKED_BY_ADDED_EVENT, BLOCKED_BY_REMOVED_EVENT, CLOSED_EVENT, REOPENED_EVENT, LABELED_EVENT, UNLABELED_EVENT]) {
    totalCount
    nodes {
      __typename
      ... on BlockedByAddedEvent { createdAt actor { login __typename } blockingIssue { number repository { nameWithOwner } } }
      ... on BlockedByRemovedEvent { createdAt actor { login __typename } blockingIssue { number repository { nameWithOwner } } }
      ... on ClosedEvent { createdAt }
      ... on ReopenedEvent { createdAt }
      ... on LabeledEvent { createdAt label { name } }
      ... on UnlabeledEvent { createdAt label { name } }
    }
  }
}'
gh api graphql --paginate -F owner='{owner}' -F name='{repo}' -f query="$list_query" >"$tmp/list.json" 2>"$tmp/gh.err" ||
	fail "gh api graphql failed: $(one_line "$tmp/gh.err")"
jq -rs '.[0].data.repository.nameWithOwner' "$tmp/list.json" >"$tmp/repo"
jq -rs '[.[].data.repository.issues.nodes[].number] | . as $n | range(0; length; 20) | $n[.:. + 20] | map("i\(.): issue(number: \(.)) { ...history }") | join(" ")' \
	"$tmp/list.json" >"$tmp/batches"
: >"$tmp/pages.json"
while read -r lookups; do
	gh api graphql -F owner='{owner}' -F name='{repo}' \
		-f query="query(\$owner: String!, \$name: String!) { repository(owner: \$owner, name: \$name) { $lookups } } $history_fields" \
		>>"$tmp/pages.json" 2>"$tmp/gh.err" ||
		fail "gh api graphql failed: $(one_line "$tmp/gh.err")"
done <"$tmp/batches"

# One history file: the repository's name, and each issue with its edits,
# open and closed states, refinement label changes and link events.
jq -s --arg running "$running_label" --rawfile repo "$tmp/repo" '{
	repo: ($repo | rtrimstr("\n")),
	issues: [.[].data.repository[] | {
		number, title,
		body: (.body // ""),
		created: .createdAt,
		author: (if .author == null then "ghost" elif .author.__typename == "Bot" then .author.login + "[bot]" else .author.login end),
		truncated: (.userContentEdits.totalCount > (.userContentEdits.nodes | length) or .timelineItems.totalCount > (.timelineItems.nodes | length)),
		edits: [.userContentEdits.nodes[] | {at: .editedAt, body: (.diff // "")}],
		states: (.closedAt as $closed | [.timelineItems.nodes[] | select(.__typename == "ClosedEvent" or .__typename == "ReopenedEvent") | {at: .createdAt, closed: (.__typename == "ClosedEvent")}]
			| if length == 0 and $closed != null then [{at: $closed, closed: true}] else . end),
		running: [.timelineItems.nodes[] | select((.__typename == "LabeledEvent" or .__typename == "UnlabeledEvent") and .label.name == $running) | {at: .createdAt, on: (.__typename == "LabeledEvent")}],
		links: [.timelineItems.nodes[] | select((.__typename == "BlockedByAddedEvent" or .__typename == "BlockedByRemovedEvent") and .blockingIssue != null) | {
			at: .createdAt, added: (.__typename == "BlockedByAddedEvent"),
			blocker: .blockingIssue.number, repo: .blockingIssue.repository.nameWithOwner,
			actor: (.actor.login // "ghost"), bot: (.actor.__typename == "Bot")
		}]
	}]
}' "$tmp/pages.json" >"$tmp/history.json"

truncated=$(jq -r '[.issues[] | select(.truncated) | "#\(.number)"] | join(", ")' "$tmp/history.json")
[ -z "$truncated" ] || fail "GitHub returned only part of the history of $truncated (more than 100 edits or timeline items); the backtest does not truncate"

# The repository at time $t, as rank.sh's candidate rule reads it.
# shellcheck disable=SC2016 # $t is a jq variable
defs='
def at_most($t): map(select(.at <= $t)) | sort_by(.at);
def open_at($t): .created <= $t and ((.states | at_most($t) | last | .closed) // false | not);
def body_at($t): (.edits | at_most($t) | last | .body) // .body;
def running_at($t): (.running | at_most($t) | last | .on) // false;
def parent_at($t): body_at($t) | capture("<!-- cw-split-plan: part of #(?<p>[0-9]+) -->").p // null | if . == null then null else tonumber end;
'

# Every current link, measured or excluded with its reason. A link is
# current when its last event is an addition, and replayed at that time.
# Y is a candidate for X at T as rank.sh would read it: open, opened by
# one of the logins, not a split parent, not X'"'"'s split parent or sibling.
jq --arg logins "$logins" --arg record "$split_record" "$defs"'
	.repo as $repo | (.issues | map({key: (.number | tostring), value: .}) | from_entries) as $by |
	($logins | split(" ") | map(select(. != ""))) as $logins |
	def why_not($x; $y; $t):
		$by[$y | tostring] as $c | ($x | parent_at($t)) as $p |
		if $c == null then "#\($y) is not among the repository'"'"'s issues"
		elif ($c | open_at($t) | not) then "#\($y) was not open then"
		elif ($logins | index($c.author)) == null then "#\($y) was opened by \($c.author), whose issues crew does not take"
		elif ($c | body_at($t) | contains($record)) then "#\($y) was a split parent then"
		elif $p != null and $y == $p then "#\($y) was the split parent of #\($x.number) then"
		elif $p != null and ($c | body_at($t) | contains("<!-- cw-split-plan: part of #\($p) -->")) then "both are parts of the split of #\($p)"
		else null end;
	[.issues[] | . as $blocked | .links | group_by([.repo, .blocker])[] | sort_by(.at) |
		{blocked: $blocked.number, blocker: .[0].blocker, repo: .[0].repo, current: (last | .added),
			at: (map(select(.added)) | last | .at), actor: (map(select(.added)) | last | .actor), bot: (map(select(.added)) | last | .bot)}] |
	sort_by(.at, .blocked, .blocker) |
	{recorded: length, removed: map(select(.current | not)) | length,
	 links: [.[] | select(.current) | . as $l |
		if $l.repo != $repo then $l + {excluded: "the blocking issue is in another repository", shown: "\($l.repo)#\($l.blocker)"}
		else
			$by[$l.blocked | tostring] as $a | $by[$l.blocker | tostring] as $b |
			(if $l.bot and $b != null and ($b | running_at($l.at)) and ($a | running_at($l.at) | not) then "blocker" else "blocked" end) as $side |
			(if $side == "blocked" then [$a, $l.blocker, 1, $b, $l.blocked, 2] else [$b, $l.blocked, 2, $a, $l.blocker, 1] end) as [$x, $y, $xs, $o, $oy, $os] |
			why_not($x; $y; $l.at) as $why |
			$l + {shown: "#\($l.blocker)", excluded: $why, refined: $x.number, counterpart: $y, section: $xs,
				other: ($o.number // null), other_counterpart: $oy, other_section: $os,
				other_ok: ($o != null and why_not($o; $oy; $l.at) == null)}
		end]}' "$tmp/history.json" >"$tmp/links.json"

measured=$(jq '[.links[] | select(.excluded == null)] | length' "$tmp/links.json")
[ "$measured" -gt 0 ] || fail "no link to measure: GitHub records no blocked_by link the shortlist could show"

# The stand-in gh answers only the two reads rank.sh makes, from the
# snapshot in $BACKTEST_SNAPSHOT, so a change in rank.sh's reads fails
# here instead of reaching the live repository.
mkdir "$tmp/standin"
cat >"$tmp/standin/gh" <<'EOF'
#!/bin/sh
if [ "$#" -eq 5 ] && [ "$1 $2 $4 $5" = "issue view --json number,title,body" ]; then
	cat "$BACKTEST_SNAPSHOT/issue.json"
elif [ "$#" -eq 10 ] && [ "$1 $2 $3 $4 $5 $7 $8 $9 ${10}" = "issue list --state open --author --limit 500 --json number,title,body" ]; then
	if [ -e "$BACKTEST_SNAPSHOT/list.$6.json" ]; then cat "$BACKTEST_SNAPSHOT/list.$6.json"; else echo '[]'; fi
else
	echo "backtest.sh: rank.sh ran a gh command the backtest cannot replay: gh $*" >&2
	exit 1
fi
EOF
chmod +x "$tmp/standin/gh"

# rank N T LINK writes rank.sh's lists for issue N at time T to $tmp/out,
# or fails naming LINK and rank.sh's own error.
rank() {
	snapshot=$tmp/snapshot
	rm -rf "$snapshot"
	mkdir "$snapshot"
	jq --argjson n "$1" --arg t "$2" "$defs"'.issues[] | select(.number == $n) | {number, title, body: body_at($t)}' \
		"$tmp/history.json" >"$snapshot/issue.json"
	for login in $logins; do
		jq --arg t "$2" --arg login "$login" "$defs"'[.issues[] | select(.author == $login and open_at($t)) | {number, title, body: body_at($t)}]' \
			"$tmp/history.json" >"$snapshot/list.$login.json"
	done
	if ! BACKTEST_SNAPSHOT=$snapshot PATH="$tmp/standin:$PATH" sh "$here/rank.sh" "$1" >"$tmp/out" 2>"$tmp/err"; then
		fail "rank.sh #$1 failed for the link $3: $(one_line "$tmp/err")"
	fi
}

# position SECTION N prints N's place in list SECTION (1 likely to block,
# 2 likely blocked by) of $tmp/out, or "not in top 5".
position() {
	place=$(awk -v sect="$1" -v want="#$2" '
		/^Likely to block / { s = 1; next }
		/^Likely blocked by / { s = 2; next }
		s == sect && $2 == want { sub(/\.$/, "", $1); print $1; exit }' "$tmp/out")
	echo "${place:-not in top $list_length}"
}

# shown SECTION prints list SECTION of $tmp/out as "#N P, #M Q".
shown() {
	awk -v sect="$1" '
		/^Likely to block / { s = 1; next }
		/^Likely blocked by / { s = 2; next }
		s == sect && /^[0-9]+\. #/ { printf "%s%s %s", sep, $2, $3; sep = ", " }
		END { print "" }' "$tmp/out"
}

: >"$tmp/rows"
: >"$tmp/misses"
found=0
model=
jq -r '.links[] | select(.excluded == null) | [.blocked, .blocker, .at, .actor, .refined, .counterpart, .section, (.other // ""), .other_counterpart, .other_section, .other_ok] | @tsv' \
	"$tmp/links.json" >"$tmp/measured.tsv"
tab=$(printf '\t')
while IFS=$tab read -r blocked blocker at actor refined counterpart section other other_counterpart other_section other_ok; do
	link="#$blocked blocked by #$blocker"
	rank "$refined" "$at" "$link"
	[ -n "$model" ] || model=$(sed -n '1s/.*, \(.*\)):$/\1/p' "$tmp/out")
	place=$(position "$section" "$counterpart")
	case $place in
	not*) printf -- '- %s, refined side #%s: %s\n' "$link" "$refined" "$(shown "$section")" >>"$tmp/misses" ;;
	*) found=$((found + 1)) ;;
	esac
	if [ "$other_ok" = true ]; then
		rank "$other" "$at" "$link"
		other_place=$(position "$other_section" "$other_counterpart")
	else
		other_place="not a candidate"
	fi
	recorded=$(printf '%s' "$at" | sed 's/T/ /; s/:[0-9]*Z$//')
	printf '| #%s | #%s | %s | %s | #%s | %s | %s |\n' "$blocked" "$blocker" "$recorded" "$actor" "$refined" "$place" "$other_place" >>"$tmp/rows"
done <"$tmp/measured.tsv"

recorded_count=$(jq .recorded "$tmp/links.json")
removed_count=$(jq .removed "$tmp/links.json")
repo=$(jq -r .repo "$tmp/history.json")
percent=$(awk -v f="$found" -v m="$measured" 'BEGIN { printf "%.1f", f * 100 / m }')
if [ $((found * 100)) -ge $((gate_percent * measured)) ]; then verdict=passed; else verdict=failed; fi

echo "## Backtest of \`/cw-rank-blockers\`"
echo
echo "\`rank.sh\` ($model) replayed against the \`blocked_by\` links GitHub records in $repo. Each link is replayed at the moment it was recorded, with the issues open then and their bodies as they read then, from the side whose refinement recorded it (the blocked issue for a link recorded by hand). A link is found when the other issue is in the top $list_length of that side's list."
echo
echo "$recorded_count links recorded: $measured measured, $(jq '[.links[] | select(.excluded != null)] | length' "$tmp/links.json") excluded, $removed_count removed since."
echo
echo "| Blocked | Blocking | Recorded (UTC) | By | Refined side | Rank on refined side | Rank on other side |"
echo "| --- | --- | --- | --- | --- | --- | --- |"
cat "$tmp/rows"
if [ -s "$tmp/misses" ]; then
	echo
	echo "Misses, with the top $list_length shown instead:"
	echo
	cat "$tmp/misses"
fi
jq -r '.links[] | select(.excluded != null) | "- #\(.blocked) blocked by \(.shown): \(.excluded)"' "$tmp/links.json" >"$tmp/excluded"
if [ -s "$tmp/excluded" ]; then
	echo
	echo "Excluded, since the shortlist could never show them:"
	echo
	cat "$tmp/excluded"
fi
echo
echo "**Gate $verdict:** $found of $measured links found ($percent%); the gate needs $gate_percent%."
