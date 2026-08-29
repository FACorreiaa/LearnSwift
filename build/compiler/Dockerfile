# The image that compiles a learner's Swift to WebAssembly.
#
# Separate from the application image on purpose. It carries a full Swift
# toolchain and two WebAssembly SDKs — about 2 GB — and none of that belongs in
# the web binary's runtime image. It is also the only place untrusted input
# meets a compiler, so it is the thing to keep small in privilege even though it
# is large on disk.
FROM swift:6.3.1

# Both SDKs, installed once at build time. Installing per compile would mean a
# 72 MB download and an unpack on every submission, which is most of the latency
# budget spent before swiftc starts.
#
# The checksum is required for a remote bundle. swift.org publishes a .sig
# rather than a .sha256, so this value was computed from a verified download —
# see the plan's Phase 2 notes. Pinning it means a substituted bundle fails the
# build rather than silently compiling learner code with an unknown toolchain.
ARG WASM_SDK_URL=https://download.swift.org/swift-6.3.1-release/wasm-sdk/swift-6.3.1-RELEASE/swift-6.3.1-RELEASE_wasm.artifactbundle.tar.gz
ARG WASM_SDK_SHA=bd47baa20771f366d8beed7970afaa30742b2210097afd15f85427226d8f4cf2

# A non-root user to compile as. The compiler is the thing being protected here:
# the program it produces never runs in this container, so the surface is
# swiftc's own handling of hostile source rather than arbitrary execution.
RUN useradd --create-home --shell /bin/bash builder

# Installed *as builder*, because swift sdk install writes to the invoking
# user's ~/.swiftpm and swiftpm looks there and nowhere else. Installing as root
# puts the SDKs somewhere the build user cannot see, and the failure surfaces as
# the unhelpful "swift-sdks doesn't exist in file system".
USER builder
RUN swift sdk install "$WASM_SDK_URL" --checksum "$WASM_SDK_SHA" \
 && swift sdk list
USER root

# Pre-warm a package skeleton so a compile does not pay for module resolution
# every time. Owned by the build user because the container runs read-only with
# only /tmp and /work writable.
RUN mkdir -p /skeleton/Sources/exercise \
 && printf '// swift-tools-version:6.0\nimport PackageDescription\nlet package = Package(name: "exercise", targets: [.executableTarget(name: "exercise", path: "Sources/exercise")])\n' > /skeleton/Package.swift \
 && chown -R builder:builder /skeleton

COPY --chown=root:root build/compiler/compile.sh /usr/local/bin/compile
RUN chmod 0555 /usr/local/bin/compile

USER builder
WORKDIR /work

ENTRYPOINT ["/usr/local/bin/compile"]
