// swift-tools-version:6.0
import PackageDescription

// swift-validate answers questions about a learner's Swift source: does it use
// this construct, does it avoid that one, and does it parse at all.
//
// It is a separate executable rather than Go code because Swift is the only
// language with a correct Swift parser. The Go side shells out to it; see
// internal/validate for the client and the reasoning.
let package = Package(
    name: "swift-validate",
    platforms: [.macOS(.v13)],
    dependencies: [
        // Pinned exactly. A parser that changes underneath the grader would
        // change what counts as a correct answer, which is not something to
        // discover from a learner's bug report.
        .package(url: "https://github.com/swiftlang/swift-syntax.git", exact: "603.0.2"),
    ],
    targets: [
        .executableTarget(
            name: "swift-validate",
            dependencies: [
                .product(name: "SwiftParser", package: "swift-syntax"),
                .product(name: "SwiftSyntax", package: "swift-syntax"),
                .product(name: "SwiftDiagnostics", package: "swift-syntax"),
                .product(name: "SwiftParserDiagnostics", package: "swift-syntax"),
            ],
            path: "Sources/swift-validate"
        ),
    ]
)
