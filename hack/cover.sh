#!/usr/bin/env bash
# Runs the unit tests of the SDK's packages with coverage into coverage.out, prints each
# package's coverage and the total, and fails when a package is under the floor (the first
# argument, a percentage; default 85). Each package is measured by its own tests.
set -euo pipefail

min="${1:-85}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

go test -covermode=atomic -coverprofile=coverage.out ./cru/... ./internal/... >/dev/null

report="$(awk '
	NR > 1 {
		pkg = $1
		sub(/:.*/, "", pkg)
		sub(/\/[^\/]*$/, "", pkg)
		stmts[pkg] += $2
		total += $2
		if ($3 > 0) {
			covered[pkg] += $2
			hit += $2
		}
	}
	END {
		for (p in stmts) printf "%s %.1f\n", p, 100 * covered[p] / stmts[p]
		printf "total %.1f\n", 100 * hit / total
	}' coverage.out | sort)"

printf '%s\n' "$report" | awk '{ printf "  %-55s %5.1f%%\n", $1, $2 }'
below="$(printf '%s\n' "$report" | awk -v min="$min" '$1 != "total" && $2 + 0 < min + 0 { print $1 }')"
if [ -n "$below" ]; then
	echo "Under the ${min}% coverage floor: $below" >&2
	exit 1
fi
