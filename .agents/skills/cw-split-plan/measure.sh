#!/bin/sh
# Measures plans for /cw-split-plan: for each file, one line with its
# characters, its distinct requirements (R1, R2, ...) and acceptance examples
# (AE1, ...), whether it is above the split threshold, and whether it is
# below the part minimum.
#
#   sh measure.sh plan.md [part.md ...]
#
# Characters are Unicode characters, CRLF counted as LF. A requirement or an
# acceptance example counts where it is defined: at the start of a line, as a
# list item or not, once however often it is defined. A mention inside a
# line, such as "Covers R5", counts nothing.
#
# The thresholds come from the cost of crew's own lfg sessions (#160): a plan
# above 10,000 characters or above 12 requirements is considered for a split,
# and no part of a split may be below 4,000 characters.
set -eu

max_characters=10000
max_requirements=12
min_part_characters=4000

# wc -m counts characters only under a UTF-8 locale; their names differ
# between Linux and macOS.
for candidate in C.UTF-8 C.utf8 en_US.UTF-8 en_US.utf8; do
	if locale -a 2>/dev/null | grep -qx "$candidate"; then
		LC_ALL=$candidate
		export LC_ALL
		break
	fi
done

if [ "$#" -eq 0 ]; then
	echo "usage: sh measure.sh FILE..." >&2
	exit 2
fi

# ids prints how many distinct IDs with prefix $1 file $2 defines.
ids() {
	tr -d '\r' <"$2" |
		grep -oE "^[[:space:]]*([-*+][[:space:]]+)?$1[0-9]+\\." |
		grep -oE "$1[0-9]+" | sort -u | wc -l | tr -d ' '
}

for file in "$@"; do
	if [ ! -r "$file" ]; then
		echo "measure.sh: cannot read $file" >&2
		exit 2
	fi
	characters=$(tr -d '\r' <"$file" | wc -m | tr -d ' ')
	requirements=$(ids R "$file")
	examples=$(ids AE "$file")
	above=no
	if [ "$characters" -gt "$max_characters" ] || [ "$requirements" -gt "$max_requirements" ]; then
		above=yes
	fi
	below=no
	if [ "$characters" -lt "$min_part_characters" ]; then
		below=yes
	fi
	echo "$file: characters=$characters requirements=$requirements acceptance_examples=$examples" \
		"above_threshold=$above below_part_minimum=$below"
done
