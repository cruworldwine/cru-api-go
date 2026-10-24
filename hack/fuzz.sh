#!/usr/bin/env bash
# Runs every fuzz target in the module, one at a time, for the given time each (default 30s).
# A failing input is written to the package's testdata/fuzz: commit it with the fix.
set -euo pipefail

fuzztime="${1:-30s}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

for pkg in $(go list ./...); do
	for target in $(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true); do
		echo "== ${pkg#"$(go list -m)"/} $target ($fuzztime)"
		go test -run '^$' -fuzz "^${target}\$" -fuzztime "$fuzztime" "$pkg"
	done
done
