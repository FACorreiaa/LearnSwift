#!/usr/bin/env bash
# Rasterise the link-preview card.
#
# og-default.png is committed rather than generated at build time: it changes
# perhaps twice a year, every scraper that matters reads PNG and almost none
# read SVG, and making a deploy depend on rsvg-convert being installed would buy
# nothing. Run this after editing og-default.svg, and commit both.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if ! command -v rsvg-convert >/dev/null 2>&1; then
  echo "rsvg-convert not found. brew install librsvg" >&2
  exit 1
fi

rsvg-convert -w 1200 -h 630 \
  web/assets/brand/og-default.svg \
  -o web/assets/brand/og-default.png

echo "OK: web/assets/brand/og-default.png rebuilt"
