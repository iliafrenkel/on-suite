#!/usr/bin/env bash
# next-version.sh suggests the next release tag, per docs/RELEASING.md's
# versioning rule: derived from the Conventional Commits (see
# CONTRIBUTING.md#commit-messages) merged since the last tag, the same way
# .goreleaser.yaml's changelog.groups already classifies them.
#
# Pre-1.0 only (current major stays 0 until all four apps exist — see
# README.md's Versioning section): a feat commit or a breaking change both
# bump minor, since there is nowhere else for "breaking" to signal while
# major is pinned at 0. Anything else release-worthy (fix/refactor/perf/
# chore/unlabeled) bumps patch. docs:/test:-only commits produce no
# suggestion, matching their exclusion from the changelog itself.
#
# This only prints a suggestion — it does not tag or push anything. Review
# it, then follow docs/RELEASING.md's own tagging steps.
set -euo pipefail

last_tag=$(git describe --tags --abbrev=0 2>/dev/null || true)
if [[ -z "$last_tag" ]]; then
	echo "no existing tag found; suggesting v0.1.0" >&2
	echo "v0.1.0"
	exit 0
fi

version="${last_tag#v}"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "error: $last_tag doesn't look like a plain vMAJOR.MINOR.PATCH tag; bump it by hand." >&2
	exit 1
fi
IFS='.' read -r major minor patch <<<"$version"
if [[ "$major" != "0" ]]; then
	echo "warning: $last_tag is >= 1.0.0 — this script only implements the pre-1.0 rule (a breaking change should bump major, not minor, past 1.0). Check docs/RELEASING.md and bump by hand." >&2
fi

# Record-separated (RS=\x1d, US=\x1e) so a multi-paragraph commit body can't
# be mistaken for more than one commit, and so a body containing blank
# lines doesn't break on IFS splitting the way a plain newline-per-commit
# format would.
commits=$(git log "${last_tag}..HEAD" --no-merges --pretty=format:'%s%x1e%b%x1d')
if [[ -z "$commits" ]]; then
	echo "no commits since $last_tag" >&2
	exit 1
fi

shopt -s nocasematch
bump=none
while IFS= read -r -d $'\x1d' record; do
	subject="${record%%$'\x1e'*}"
	body="${record#*$'\x1e'}"

	if [[ "$subject" =~ ^docs(\(.+\))?:.* ]] || [[ "$subject" =~ ^test(\(.+\))?:.* ]]; then
		continue # excluded from the changelog itself; no release signal
	fi

	if [[ "$subject" =~ ^feat(\(.+\))?\!?: ]] ||
		[[ "$subject" =~ ^[a-z]+(\(.+\))?\!: ]] ||
		[[ "$body" =~ BREAKING[-\ ]CHANGE ]]; then
		bump=minor
		continue
	fi

	if [[ "$bump" == "none" ]]; then
		bump=patch
	fi
done <<<"$commits"
shopt -u nocasematch

case "$bump" in
none)
	echo "only docs:/test: commits since $last_tag; no release needed" >&2
	exit 1
	;;
minor)
	echo "v${major}.$((minor + 1)).0"
	;;
patch)
	echo "v${major}.${minor}.$((patch + 1))"
	;;
esac
