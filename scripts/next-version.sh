#!/usr/bin/env bash
# next-version.sh suggests the next release tag, per docs/developers/releasing.md's
# versioning rule (see docs/developers/releasing.md#versioning): derived from
# the Conventional Commits (see CONTRIBUTING.md#commit-messages) merged since
# the last tag, the same way .goreleaser.yaml's changelog.groups already
# classifies them.
#
# Past 1.0, ordinary semver: a breaking change (`BREAKING CHANGE:` footer or
# `type!:`) bumps major, any `feat:` (with no breaking change) bumps minor,
# and anything else release-worthy (fix/refactor/perf/chore/unlabeled) bumps
# patch. Pre-1.0 (major stays 0 until all four apps exist), a feat commit or
# a breaking change both bump minor instead, since there is nowhere else for
# "breaking" to signal while major is pinned at 0 — that's how the 0.x
# history was tagged, so it's kept as-is rather than reinterpreted. Either
# way, docs:/test:-only commits produce no suggestion, matching their
# exclusion from the changelog itself. The highest bump across all commits
# since the last tag wins, so a breaking change isn't downgraded by a later
# feat commit.
#
# This only prints a suggestion — it does not tag or push anything. Review
# it, then follow docs/developers/releasing.md's own tagging steps.
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

# Record-separated (RS=\x1d, US=\x1e) so a multi-paragraph commit body can't
# be mistaken for more than one commit, and so a body containing blank
# lines doesn't break on IFS splitting the way a plain newline-per-commit
# format would.
commits=$(git log "${last_tag}..HEAD" --no-merges --pretty=format:'%s%x1e%b%x1d')
if [[ -z "$commits" ]]; then
	echo "no commits since $last_tag" >&2
	exit 1
fi

# Numeric so the highest bump across all commits wins, e.g. a breaking
# change followed by a later feat: commit must stay a major bump, not get
# downgraded to minor. 0=none, 1=patch, 2=minor, 3=major.
shopt -s nocasematch
bump_level=0
while IFS= read -r -d $'\x1d' record; do
	# git inserts its own "\n" between each --pretty=format record, which
	# lands as a leading newline on every record but the first — strip it so
	# the subject regexes below (anchored with ^) still match.
	record="${record#$'\n'}"
	subject="${record%%$'\x1e'*}"
	body="${record#*$'\x1e'}"

	if [[ "$subject" =~ ^docs(\(.+\))?:.* ]] || [[ "$subject" =~ ^test(\(.+\))?:.* ]]; then
		continue # excluded from the changelog itself; no release signal
	fi

	if [[ "$subject" =~ ^[a-z]+(\(.+\))?\!: ]] ||
		[[ "$body" =~ BREAKING[-\ ]CHANGE ]]; then
		if [[ "$major" == "0" ]]; then
			level=2 # pre-1.0: breaking has nowhere to signal but minor
		else
			level=3
		fi
	elif [[ "$subject" =~ ^feat(\(.+\))?:.* ]]; then
		level=2
	else
		level=1
	fi

	if ((level > bump_level)); then
		bump_level=$level
	fi
done <<<"$commits"
shopt -u nocasematch

case "$bump_level" in
0)
	echo "only docs:/test: commits since $last_tag; no release needed" >&2
	exit 1
	;;
3)
	echo "v$((major + 1)).0.0"
	;;
2)
	echo "v${major}.$((minor + 1)).0"
	;;
1)
	echo "v${major}.${minor}.$((patch + 1))"
	;;
esac
