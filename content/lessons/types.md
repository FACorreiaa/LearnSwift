---
title: Learn Type Inference in 3 Minutes
track: swift-basics
order: 2
minutes: 3
summary: Swift works out the type, until you need to tell it.
runtime: embedded
starter: |
  let count = 42          // inferred as Int
  let pi = 3.14           // inferred as Double

  // Make `total` a Double so the division below is not integer division.
  let total = 7
  print(total / 2)
hint: >-
  The literal `7` is an integer, and integer division throws away the
  remainder. Give the constant the type you actually want before the
  division happens.
solution: |
  let count = 42
  let pi = 3.14

  let total: Double = 7
  print(total / 2)
assertions:
  must_declare:
    - "total: Double"
  output_contains:
    - "3.5"
---

## Concept

Swift is statically typed, but you rarely write the type down. It is inferred
from the value:

```swift
let count = 42          // Int
let pi = 3.14           // Double
let name = "Fernando"   // String
let ready = true        // Bool
```

The type is fixed at that point and checked forever after. Inference is about
saving keystrokes, not about being loose:

```swift
var count = 42
count = "forty-two"   // error: cannot assign String to Int
```

## When you have to say it

Write the type when the literal alone would give you the wrong one:

```swift
let total = 7            // Int
let total: Double = 7    // Double
```

That distinction matters more in Swift than in most languages, because Swift
does **no** implicit numeric conversion:

```swift
let a = 7
let b = 2.0
print(a / b)             // error: cannot divide Int by Double
print(Double(a) / b)     // 3.5
```

It also matters for integer division, which silently truncates:

```swift
print(7 / 2)             // 3   — both are Int
print(7.0 / 2)           // 3.5 — the literal made it Double
```

And you must say it when there is no value yet to infer from:

```swift
var items: [String] = []
let scores: [String: Int] = [:]
```

## Exercise

Make `total` a `Double` so `total / 2` gives `3.5` rather than `3`.
