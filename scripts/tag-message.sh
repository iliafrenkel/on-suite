#!/bin/sh
# scripts/tag-message.sh VERSION
#
# Prints the release tag message for VERSION from docs/user/changelog.md:
# the top entry's title, a blank line, then its summary (without the date
# line). The top entry must be VERSION, so a tag can't go out with last
# release's notes. Use it when tagging (see docs/developers/releasing.md):
#
#   scripts/tag-message.sh 6.5.0 | git tag -a v6.5.0 -F -
set -eu

if [ $# -ne 1 ] || [ -z "$1" ]; then
	echo "usage: $0 VERSION   (e.g. 6.5.0 or v6.5.0)" >&2
	exit 2
fi
want=${1#v}

changelog=$(cd "$(dirname "$0")/.." && pwd)/docs/user/changelog.md
[ -f "$changelog" ] || { echo "$0: $changelog not found" >&2; exit 1; }

# The first "## N.N.N: Title" heading and everything up to the next one.
entry=$(awk '
	/^## / { if (seen) exit; seen = 1; print; next }
	seen   { print }
' "$changelog")

heading=$(printf '%s\n' "$entry" | head -n 1)
got=$(printf '%s\n' "$heading" | sed -n 's/^## \([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\): .*/\1/p')
title=$(printf '%s\n' "$heading" | sed -n 's/^## [0-9.]*: \(.*\)/\1/p')

if [ -z "$got" ] || [ -z "$title" ]; then
	echo "$0: the top entry of $changelog is not a \"## N.N.N: Title\" heading" >&2
	exit 1
fi
if [ "$got" != "$want" ]; then
	echo "$0: the changelog's top entry is $got, not $want — add the $want entry first" >&2
	exit 1
fi

# Body: drop the heading and the *date* line, trim leading/trailing blanks.
body=$(printf '%s\n' "$entry" | tail -n +2 | grep -v '^\*[0-9][0-9]* [A-Z][a-z]* [0-9][0-9]*\*$' |
	awk 'NF { started = 1 } started { lines[++n] = $0 } END { while (n > 0 && lines[n] == "") n--; for (i = 1; i <= n; i++) print lines[i] }')

printf '%s\n\n%s\n' "$title" "$body"
