import Foundation
import SwiftDiagnostics
import SwiftParser
import SwiftParserDiagnostics
import SwiftSyntax

// swift-validate reads one JSON request on stdin and writes one JSON response
// on stdout. One shot per invocation: the caller is a web request handler, the
// inputs are small, and a long-lived process would need a protocol, a health
// check and a restart policy to earn its keep.

// MARK: - Wire format

struct Assertions: Decodable {
    var mustDeclare: [String] = []
    var mustNotUse: [String] = []

    enum CodingKeys: String, CodingKey {
        case mustDeclare = "must_declare"
        case mustNotUse = "must_not_use"
    }

    // Written out rather than synthesized. Swift's generated decoder requires
    // every key whose property is not Optional — a default value does not make
    // one optional — so a lesson declaring only `must_not_use` would be
    // rejected as a malformed request rather than validated. An absent key
    // means "no assertions of this kind", which is the only sane reading.
    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        mustDeclare = try container.decodeIfPresent([String].self, forKey: .mustDeclare) ?? []
        mustNotUse = try container.decodeIfPresent([String].self, forKey: .mustNotUse) ?? []
    }
}

struct Request: Decodable {
    let source: String
    let assertions: Assertions

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        source = try container.decode(String.self, forKey: .source)
        // Likewise: a request with no assertions block at all is a request to
        // check only that the source parses.
        assertions = try container.decodeIfPresent(Assertions.self, forKey: .assertions) ?? Assertions()
    }

    enum CodingKeys: String, CodingKey {
        case source, assertions
    }
}

struct Failure: Encodable {
    let kind: String
    let token: String
    let message: String
}

struct Diagnostic: Encodable {
    let line: Int
    let column: Int
    let message: String
}

struct Response: Encodable {
    let ok: Bool
    let failures: [Failure]
    let diagnostics: [Diagnostic]
    /// The source with comments and string contents blanked out, which is what
    /// the token assertions were evaluated against. Returned so a failing
    /// assertion can be explained rather than merely asserted.
    let codeOnly: String

    enum CodingKeys: String, CodingKey {
        case ok, failures, diagnostics
        case codeOnly = "code_only"
    }
}

// MARK: - Code extraction

/// Returns the source with everything that is not code blanked out, preserving
/// every byte offset so line and column numbers still line up.
///
/// This is the whole reason the validator is written in Swift. An assertion
/// like `must_not_use: "!"` is a question about the learner's *code*, and a
/// substring search cannot tell the force-unwrap in `x!` from the exclamation
/// mark in `print("Hello!")`. The parser can: comments arrive as trivia and
/// never appear as tokens at all, and the text inside a string literal arrives
/// as a `.stringSegment` token that is skipped here.
func codeOnly(_ source: String) -> String {
    let tree = Parser.parse(source: source)

    // Start from an all-blank buffer of the same length, then copy back only
    // the bytes belonging to code tokens. Blanking rather than deleting keeps
    // offsets intact, so a diagnostic's line number still means something.
    var result = Array(source.utf8).map { byte -> UInt8 in
        // Newlines survive so line numbers are preserved.
        byte == UInt8(ascii: "\n") ? byte : UInt8(ascii: " ")
    }

    for token in tree.tokens(viewMode: .sourceAccurate) {
        // The contents of a string literal are not code. The quotes around it
        // are kept, so `let s = ""` still reads as an assignment.
        if case .stringSegment = token.tokenKind { continue }

        let start = token.positionAfterSkippingLeadingTrivia.utf8Offset
        let bytes = Array(token.text.utf8)
        guard start >= 0, start + bytes.count <= result.count else { continue }

        for (i, byte) in bytes.enumerated() {
            result[start + i] = byte
        }
    }

    return String(decoding: result, as: UTF8.self)
}

// MARK: - Diagnostics

func syntaxDiagnostics(_ source: String) -> [Diagnostic] {
    let tree = Parser.parse(source: source)
    let converter = SourceLocationConverter(fileName: "exercise.swift", tree: tree)

    return ParseDiagnosticsGenerator.diagnostics(for: tree).map { diag in
        let location = diag.location(converter: converter)
        return Diagnostic(
            line: location.line,
            column: location.column,
            message: diag.message
        )
    }
}

// MARK: - Entry point

func main() {
    let input = FileHandle.standardInput.readDataToEndOfFile()

    let request: Request
    do {
        request = try JSONDecoder().decode(Request.self, from: input)
    } catch {
        FileHandle.standardError.write(Data("swift-validate: bad request: \(error)\n".utf8))
        exit(2)
    }

    let code = codeOnly(request.source)
    var failures: [Failure] = []

    for token in request.assertions.mustDeclare where !code.contains(token) {
        failures.append(Failure(
            kind: "must_declare",
            token: token,
            message: "Your code needs to use \(token)."
        ))
    }

    for token in request.assertions.mustNotUse where code.contains(token) {
        failures.append(Failure(
            kind: "must_not_use",
            token: token,
            message: "This exercise asks you not to use \(token)."
        ))
    }

    let diagnostics = syntaxDiagnostics(request.source)

    let response = Response(
        // Syntax errors fail the exercise on their own: code that does not
        // parse cannot be said to have satisfied anything.
        ok: failures.isEmpty && diagnostics.isEmpty,
        failures: failures,
        diagnostics: diagnostics,
        codeOnly: code
    )

    do {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        FileHandle.standardOutput.write(try encoder.encode(response))
    } catch {
        FileHandle.standardError.write(Data("swift-validate: cannot encode response: \(error)\n".utf8))
        exit(2)
    }
}

main()
