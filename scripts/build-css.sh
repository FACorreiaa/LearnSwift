#!/usr/bin/env bash
# Build the Tailwind CSS bundle into web/assets/css/output.css.
#
# Required before any `go build`/`go test`: the CSS is generated, not committed
# (see .gitignore), and web/assets embeds the css/ directory, so a missing build
# fails at compile time rather than silently serving an unstyled app.
#
# This is the single source of truth for the CSS build — the Taskfile, CI and
# the Dockerfile all call this script rather than repeating the command, so the
# three can never drift apart.
#
# Unlike North's equivalent, this scans only local sources. Every templUI
# component is vendored into web/shared/ui, so there is no module elsewhere in
# the build cache holding templates Tailwind needs to see.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v tailwindcss >/dev/null 2>&1; then
  echo "tailwindcss not found on PATH" >&2
  exit 1
fi

printf '%s\n' \
  '@source "../../../web/**/*.templ";' \
  '@source "../../../web/**/*.js";' \
  > ./web/assets/css/sources.generated.css

echo "running tailwindcss…"
tailwindcss -i ./web/assets/css/input.css -o ./web/assets/css/output.css --minify

if [[ ! -s ./web/assets/css/output.css ]]; then
  echo "tailwindcss produced an empty output.css" >&2
  exit 1
fi

echo "OK: wrote web/assets/css/output.css ($(wc -c < ./web/assets/css/output.css) bytes)"
