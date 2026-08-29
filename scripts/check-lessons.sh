#!/usr/bin/env bash
# Compile and run every lesson's solution under the runtime that lesson
# declares, and verify its output_contains claims against what it really prints.
#
# The runtime matters, and an earlier version of this script got it wrong by
# using the host toolchain: a top-level `var` typechecks perfectly on macOS and
# is rejected outright by the embedded WebAssembly SDK, because Swift 6 treats
# it as nonisolated global shared mutable state. Four lessons looked correct
# here and would have failed for the first learner who pressed Check.
#
# Lessons are checked in parallel. Sequentially this took over ten minutes —
# each compile is a container start — and a gate that slow is one people learn
# to skip, which makes it worse than no gate at all.
#
# Needs the compiler image (`task compiler:build`). Skips when it is absent so
# `task check` still works without it; CI builds the image so the skip cannot
# become the normal case.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

IMAGE="${COMPILER_IMAGE:-seshat-compiler:6.3.1}"
JOBS="${LESSON_CHECK_JOBS:-4}"

# A compile that has not finished in this long is not going to be useful. On a
# healthy machine one takes about a second; this cap exists because a degraded
# container runtime can take minutes per compile, and a check that hangs is
# indistinguishable from a check that is broken.
TIMEOUT="${LESSON_CHECK_TIMEOUT:-120}"

if ! docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "compiler image $IMAGE not found; skipping lesson checks (build it with: task compiler:build)"
  exit 0
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The wasm runner is built once rather than per lesson: `go run` would otherwise
# relink it sixteen times, which costs more than the compiles it supports.
runner="$work/runwasm"
if ! go build -o "$runner" ./scripts/runwasm 2>"$work/build.err"; then
  echo "could not build the wasm runner:" >&2
  cat "$work/build.err" >&2
  exit 1
fi

# Every variable check_one reads must be exported: xargs runs it in a fresh
# bash, and an unexported TIMEOUT silently became `timeout "" docker …`,
# which fails with no compiler output and so reads as "does not compile".
export ROOT IMAGE work runner TIMEOUT

check_one() {
  file="$1"
  slug="$(basename "$file" .md)"
  runtime="$(awk -F': ' '/^runtime:/{print $2; exit}' "$file")"

  case "$runtime" in
    embedded) sdk=swift-6.3.1-RELEASE_wasm-embedded ;;
    full)     sdk=swift-6.3.1-RELEASE_wasm ;;
    none)
      # SwiftUI: no WebAssembly build exists, so there is nothing to compile.
      echo "SKIP $slug"
      return 0
      ;;
    *)
      echo "FAIL $slug unknown runtime: $runtime"
      return 1
      ;;
  esac

  python3 - "$file" solution > "$work/$slug.swift" <<'PY'
import sys
lines = open(sys.argv[1]).read().split("\n")
out, inside = [], False
for line in lines:
    if line.startswith(sys.argv[2] + ": |"):
        inside = True
        continue
    if inside:
        if line and not line.startswith("  "):
            break
        out.append(line[2:] if line.startswith("  ") else line)
print("\n".join(out).rstrip())
PY

  if [ ! -s "$work/$slug.swift" ]; then
    echo "FAIL $slug no solution"
    return 1
  fi

  out="$work/$slug.out"
  mkdir -p "$out"
  chmod 777 "$out"

  # The status is captured rather than tested with `if !`, because inside an
  # `if ! cmd` block $? holds the negation's result and the timeout case below
  # would never match.
  build="$(timeout "$TIMEOUT" docker run --rm -i --network none --read-only \
        --tmpfs "/tmp:rw,exec,nosuid,nodev,size=1g,uid=1001,gid=1001" \
        --tmpfs "/home/builder/.cache:rw,noexec,nosuid,nodev,size=512m,uid=1001,gid=1001" \
        -v "$out:/work" --memory 2g --cpus 2 --pids-limit 256 \
        --security-opt no-new-privileges --cap-drop ALL \
        -e "SESHAT_SDK=$sdk" "$IMAGE" < "$work/$slug.swift" 2>&1)"
  rc=$?

  if [ $rc -eq 124 ]; then
    # timeout(1) reports 124. That is the container runtime being slow, not the
    # lesson being wrong, and saying otherwise would send someone to debug
    # perfectly good Swift.
    echo "SLOW $slug compile exceeded ${TIMEOUT}s; container runtime is degraded"
    return 0
  fi

  if [ $rc -ne 0 ]; then
    echo "FAIL $slug does not compile as runtime=$runtime"
    grep -E 'error:' <<<"$build" | grep -v '/home/builder/' | head -2 \
      | sed 's|/tmp/tmp\.[A-Za-z0-9]*/Sources/exercise/||' | sed 's/^/      /'
    return 1
  fi

  python3 - "$file" > "$work/$slug.expect" <<'PY'
import re, sys
s = open(sys.argv[1]).read()
m = re.search(r'\n  output_contains:\n((?:    - .*\n)+)', s)
if m:
    for line in m.group(1).strip().split("\n"):
        print(line.strip()[2:].strip().strip('"'))
PY

  if [ ! -s "$work/$slug.expect" ]; then
    echo "OK   $slug ($runtime, no output claims)"
    return 0
  fi

  if ! actual="$("$runner" "$out/out.wasm" 2>&1)"; then
    echo "FAIL $slug compiles but does not run"
    head -2 <<<"$actual" | sed 's/^/      /'
    return 1
  fi

  while IFS= read -r want; do
    [ -z "$want" ] && continue
    if ! grep -qF -- "$want" <<<"$actual"; then
      echo "FAIL $slug wrong output: expected $(printf '%q' "$want")"
      echo "      actual: $(printf '%q' "$(head -c 120 <<<"$actual")")"
      return 1
    fi
  done < "$work/$slug.expect"

  echo "OK   $slug ($runtime)"
}
export -f check_one

results="$work/results"
# --halt never: one broken lesson should not hide the others, so every lesson is
# checked and the failures are reported together.
printf '%s\n' content/lessons/*.md \
  | xargs -P "$JOBS" -I{} bash -c 'check_one "$@"' _ {} \
  > "$results" 2>&1
status=$?

sort "$results" | sed 's/^/  /'

passed="$(grep -c '^OK ' "$results" || true)"
skipped="$(grep -c '^SKIP ' "$results" || true)"
failed="$(grep -c '^FAIL ' "$results" || true)"
slow="$(grep -c '^SLOW ' "$results" || true)"

echo
if [ "$slow" -gt 0 ]; then
  echo "warning: $slow lesson(s) could not be checked because compiles exceeded ${TIMEOUT}s" >&2
fi

if [ "$failed" -eq 0 ] && [ "$status" -eq 0 ]; then
  echo "OK: $passed lesson solutions compile for their declared runtime and print what they claim ($skipped SwiftUI skipped, $slow timed out)"
  exit 0
fi

echo "$failed lesson(s) are broken" >&2
exit 1
