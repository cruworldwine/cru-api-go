#!/usr/bin/env bash
# Reports the exported API changes of the working tree against a base revision (the first
# argument; default origin/main), with golang.org/x/exp/cmd/apidiff at APIDIFF_VERSION. It fails
# on an incompatible change unless ALLOW_BREAKING is 1 or true. REPORT, if set, names a file the
# report is appended to as Markdown, such as $GITHUB_STEP_SUMMARY.
set -euo pipefail

base="${1:-origin/main}"
: "${APIDIFF_VERSION:?set APIDIFF_VERSION, as the Makefile does}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

apidiff() { go run "golang.org/x/exp/cmd/apidiff@${APIDIFF_VERSION}" "$@"; }
module="$(go list -m)"
tmp="$(mktemp -d)"
trap 'git worktree remove --force "$tmp/base" >/dev/null 2>&1 || true; rm -rf "$tmp"' EXIT

# quietly runs apidiff in the directory $1, showing its notes on the internal packages it skips
# only if it fails.
quietly() {
	local dir="$1"
	shift
	if ! (cd "$dir" && apidiff "$@") 2>"$tmp/apidiff.log"; then
		cat "$tmp/apidiff.log" >&2
		exit 1
	fi
}

git worktree add --quiet --detach "$tmp/base" "$base"
quietly "$tmp/base" -m -w "$tmp/base.api" "$module"
quietly "$root" -m -w "$tmp/head.api" "$module"
quietly "$root" -m "$tmp/base.api" "$tmp/head.api" >"$tmp/changes.txt"
quietly "$root" -m -incompatible "$tmp/base.api" "$tmp/head.api" >"$tmp/incompatible.txt"

if [ -s "$tmp/changes.txt" ]; then
	echo "Exported API changes against $base:"
	cat "$tmp/changes.txt"
else
	echo "No exported API changes against $base."
fi
if [ -n "${REPORT:-}" ]; then
	{
		echo "## Exported API changes against $base"
		echo
		if [ -s "$tmp/changes.txt" ]; then
			echo '```'
			cat "$tmp/changes.txt"
			echo '```'
		else
			echo "None."
		fi
	} >>"$REPORT"
fi

case "${ALLOW_BREAKING:-}" in
1 | true) exit 0 ;;
esac
# Version's value changes in every release, which breaks no caller: only the other lines count.
if grep -v -E '^(Incompatible changes:|- \./cru\.Version: value changed from "[^"]*" to "[^"]*")$' "$tmp/incompatible.txt" | grep -q '[^[:space:]]'; then
	echo "Incompatible changes to the exported API. If they are intended, label the pull request breaking-change and say in CHANGELOG.md what users must change." >&2
	exit 1
fi
