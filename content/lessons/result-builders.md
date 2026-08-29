---
title: Learn Result Builders in 5 Minutes
track: patterns
order: 4
minutes: 5
summary: The compiler feature that makes SwiftUI's body look like a language.
runtime: embedded
starter: |
  // Make this a result builder, so the closure below can list values without
  // commas or an explicit array.
  struct SentenceBuilder {
  }

  let sentence = ["Hello", "World"].joined(separator: " ")
  print(sentence)
solution: |
  @resultBuilder
  struct SentenceBuilder {
      static func buildBlock(_ parts: String...) -> String {
          parts.joined(separator: " ")
      }
  }

  func make(@SentenceBuilder _ build: () -> String) -> String {
      build()
  }

  let sentence = make {
      "Hello"
      "World"
  }
  print(sentence)
assertions:
  must_declare:
    - "@resultBuilder"
    - "buildBlock"
  output_contains:
    - "Hello World"
---

## Concept

This is legal Swift:

```swift
VStack {
    Text("Hello")
    Text("World")
}
```

Two expressions on separate lines, no commas, no array, no `return` — and it
produces one value. That is a **result builder**, and it is a general language
feature rather than anything SwiftUI-specific.

```swift
@resultBuilder
struct SentenceBuilder {
    static func buildBlock(_ parts: String...) -> String {
        parts.joined(separator: " ")
    }
}
```

Apply it to a closure parameter:

```swift
func make(@SentenceBuilder _ build: () -> String) -> String {
    build()
}

let sentence = make {
    "Hello"
    "World"
}
// "Hello World"
```

The compiler collects the statements in the closure and passes them to
`buildBlock`.

## Supporting if and for

A bare `buildBlock` handles a list. Control flow needs more methods, which is
why `if` inside a `VStack` works at all:

```swift
extension SentenceBuilder {
    static func buildOptional(_ part: String?) -> String { part ?? "" }
    static func buildEither(first: String) -> String { first }
    static func buildEither(second: String) -> String { second }
    static func buildArray(_ parts: [String]) -> String {
        parts.joined(separator: " ")
    }
}
```

- `buildOptional` — an `if` with no `else`
- `buildEither` — the branches of an `if`/`else`
- `buildArray` — a `for` loop

Leave one out and that construct simply will not compile inside the builder,
which is why SwiftUI occasionally rejects control flow you expected to work.

## Why this matters beyond SwiftUI

The pattern — a closure that reads as a declarative list — suits anything with
nested structure: a regex builder, a test-fixture builder, a query builder. Once
you recognise `@resultBuilder`, SwiftUI's syntax stops looking like a special
case and starts looking like an ordinary use of the language.

## Exercise

Make `SentenceBuilder` a result builder so the closure can list words without
commas.
