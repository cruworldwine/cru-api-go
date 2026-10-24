#!/usr/bin/env bash
# Fails if regenerating the SDK changes anything: a generated file was edited by hand, or the spec
# or the generator moved on without a regeneration. It compares every file before and after, so
# it works on a dirty tree too, and names the files regeneration changed.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

snapshot() {
	git ls-files -z --cached --others --exclude-standard | while IFS= read -r -d '' file; do
		if [ -f "$file" ]; then
			shasum -a 256 "$file"
		else
			echo "missing  $file"
		fi
	done | sort -k2
}

before="$(snapshot)"
hack/generate.sh >/dev/null
after="$(snapshot)"
if [ "$before" != "$after" ]; then
	echo "Regenerating changed these files; the generated code is out of date or was edited by hand:" >&2
	diff <(printf '%s\n' "$before") <(printf '%s\n' "$after") | awk '/^[<>]/ { print "  " $NF }' | sort -u >&2
	exit 1
fi
echo "The generated code is up to date."
