#!/bin/sh
set -eu

module_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../../../tsc" && pwd)
cd "$module_dir"
exec go run ./cmd/astbench "$@"
