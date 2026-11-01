#!/usr/bin/env sh
# Pinned package manager, no global tool installation or runtime Node service.
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec npm exec --yes --package=pnpm@10.34.9 -- pnpm --dir "$project_dir/frontend" "$@"
