---
title: Learn Functions in 4 Minutes
track: swift-basics
order: 5
minutes: 4
summary: Why Swift calls read like sentences, and what the underscore does.
runtime: embedded
starter: |
  // Give the parameter an external label of `to`, so the call site reads
  // greet(to: "World"), then call it.
  func greet(name: String) -> String {
      "Hello, \(name)!"
  }

  print(greet(name: "World"))
solution: |
  func greet(to name: String) -> String {
      "Hello, \(name)!"
  }

  print(greet(to: "World"))
assertions:
  must_declare:
    - "to name: String"
  output_contains:
    - "Hello, World!"
---

## Concept

```swift
func greet(name: String) -> String {
    return "Hello, \(name)!"
}

print(greet(name: "World"))
```

A single-expression function can drop the `return`:

```swift
func greet(name: String) -> String {
    "Hello, \(name)!"
}
```

## Argument labels

This is the part that surprises people arriving from other languages: Swift
calls name their arguments, and a parameter can have **two** names — one used
by the caller, one used inside the body.

```swift
func greet(to name: String) -> String {
    "Hello, \(name)!"      // `name` inside
}

greet(to: "World")         // `to` outside
```

The point is that the call site reads as a phrase. Compare:

```swift
move(from: start, to: end)
move(start, end)
```

The first cannot be got backwards by a reader.

Use `_` when a label adds nothing — usually when the function name already says
what the argument is:

```swift
func double(_ value: Int) -> Int { value * 2 }

double(21)      // not double(value: 21)
```

## Defaults

A parameter with a default can be left out entirely, which removes most of the
need for overloads:

```swift
func greet(_ name: String, punctuation: String = "!") -> String {
    "Hello, \(name)\(punctuation)"
}

greet("World")                      // Hello, World!
greet("World", punctuation: "?")    // Hello, World?
```

## Exercise

Give `greet` an external label of `to`, so the call reads `greet(to: "World")`.
