#!/usr/bin/env bash
# Compile the Swift source on stdin to WebAssembly, writing the module to
# /work/out.wasm and any diagnostics to stderr.
#
# Source arrives on stdin rather than as an argument so that nothing a learner
# writes is ever interpreted by a shell.
set -uo pipefail

sdk="${SESHAT_SDK:-swift-6.3.1-RELEASE_wasm-embedded}"

case "$sdk" in
  swift-6.3.1-RELEASE_wasm|swift-6.3.1-RELEASE_wasm-embedded) ;;
  *) echo "compile: unknown SDK: $sdk" >&2; exit 2 ;;
esac

# /tmp is a tmpfs supplied by the caller; the image itself is read-only.
build="$(mktemp -d)"
trap 'rm -rf "$build"' EXIT

cp -r /skeleton/. "$build/"
cat > "$build/Sources/exercise/main.swift"

cd "$build" || exit 2

# --strip-all is the only linker flag measured to reduce artifact size
# meaningfully: -Osize changed nothing, stripping cut roughly 15% off the
# compressed module.
if ! swift build --swift-sdk "$sdk" -c release \
      -Xlinker --strip-all \
      --scratch-path "$build/.build" 2>&1; then
  exit 1
fi

module="$(find "$build/.build" -name '*.wasm' -type f | head -1)"
if [[ -z "$module" ]]; then
  echo "compile: the build reported success but produced no module" >&2
  exit 2
fi

cp "$module" /work/out.wasm
