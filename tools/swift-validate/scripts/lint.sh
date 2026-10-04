#!/bin/sh
# Check formatting, SwiftLint, and unused code. Does not rewrite files.
set -eu
cd "$(dirname "$0")/.."
swiftformat . --lint
swiftlint
periphery scan
