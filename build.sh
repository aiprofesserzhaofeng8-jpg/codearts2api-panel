#!/usr/bin/env sh
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$project_dir"
mkdir -p bin
"${CODEARTS_GO_BIN:-go}" build -trimpath -ldflags '-s -w -X main.version=0.1.0-panel' -o bin/codearts2api-panel .
printf '%s\n' 'Built bin/codearts2api-panel. Run ./start.sh to start the gateway.'
