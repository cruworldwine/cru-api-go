#!/usr/bin/env bash
# Regenerates the SDK's API surface (cru/*.go marked "Code generated", the replay's call table,
# api/openapi.yaml and the README's method table) with the generator in Cru's API repository.
#
#   CRU_API_REPO=/path/to/cru-api-checkout make generate
#
# CRU_API_REPO is the checkout holding docs/api/tools/sdkgen, or its parent. It has no default:
# the generator is not part of this repository.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [ -z "${CRU_API_REPO:-}" ]; then
	echo "hack/generate.sh: set CRU_API_REPO to the checkout of Cru's API repository (holding docs/api/tools/sdkgen)" >&2
	exit 1
fi
generator=""
for candidate in "$CRU_API_REPO/docs/api/tools/sdkgen/generate.py" "$CRU_API_REPO/web-root/docs/api/tools/sdkgen/generate.py"; do
	if [ -f "$candidate" ]; then
		generator="$candidate"
		break
	fi
done
if [ -z "$generator" ]; then
	echo "hack/generate.sh: no docs/api/tools/sdkgen/generate.py under $CRU_API_REPO" >&2
	exit 1
fi
if ! command -v uv >/dev/null 2>&1; then
	echo "hack/generate.sh: the generator runs with uv (https://docs.astral.sh/uv/); install it" >&2
	exit 1
fi

PYTHONDONTWRITEBYTECODE=1 uv run --quiet --with pyyaml==6.0.3 python "$generator" --go "$root"
