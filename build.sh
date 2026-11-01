#!/usr/bin/env sh
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
"$project_dir/scripts/frontend.sh" install --frozen-lockfile
"$project_dir/scripts/frontend.sh" check
"$project_dir/scripts/frontend.sh" build
mkdir -p "$project_dir/bin"
cd "$project_dir/backend"
"${CODEARTS_GO_BIN:-go}" build -trimpath -ldflags "-s -w -X main.version=${CODEARTS_VERSION:-0.3.0-dev}" -o "$project_dir/bin/codearts2api-panel" ./cmd/panel
printf '%s\n' 'Built bin/codearts2api-panel. Run ./start.sh to start the gateway.'
