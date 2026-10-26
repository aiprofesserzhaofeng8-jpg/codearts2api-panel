#!/usr/bin/env sh
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$project_dir"
if [ ! -x bin/codearts2api-panel ]; then
  ./build.sh
fi
exec ./bin/codearts2api-panel -data "$project_dir/data/state.json" -no-import "$@"
