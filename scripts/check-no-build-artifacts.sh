#!/usr/bin/env bash
# Fail if a generated file has been committed.
#
# templ output, the Tailwind bundle and the swift-validate SwiftPM build
# directory are built from source on every machine that builds the app.
# Committing one means a stale copy can win a merge and then be served — a bug
# that reads as "my change did nothing" and wastes an afternoon before anyone
# suspects the file in git. A committed .build also breaks a clean CI checkout
# outright: SwiftPM finds its own state without the checkouts it names.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

artifacts=$(git ls-files -- \
  '*_templ.go' \
  'web/assets/css/output.css' \
  'web/assets/css/sources.generated.css' \
  'tools/swift-validate/.build/*')

if [[ -n "$artifacts" ]]; then
  echo "generated files are committed; they belong in .gitignore:" >&2
  echo "$artifacts" >&2
  exit 1
fi

echo "OK: no generated files are tracked"
