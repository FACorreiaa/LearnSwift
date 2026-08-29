---
title: Learn let and var in 2 Minutes
track: swift-basics
order: 1
minutes: 2
summary: Two keywords, and a default that shapes everything else.
runtime: embedded
starter: |
  func report() {
      var total = 10
      total = 20

      // `name` never changes. Declare it so the compiler knows that.
      var name = "Fernando"
      print("\(name): \(total)")
  }

  report()
solution: |
  func report() {
      var total = 10
      total = 20

      let name = "Fernando"
      print("\(name): \(total)")
  }

  report()
assertions:
  must_declare:
    - "let name"
  output_contains:
    - "Fernando: 20"
---

## Concept

`var` makes a variable you can change. `let` makes one you cannot.

```swift
var score = 0
score = 10        // fine

let name = "Fernando"
name = "Someone"  // error: cannot assign to value: 'name' is a 'let' constant
```

That is the whole syntax. The interesting part is which one you reach for.

## Reach for let first

Swift's convention is `let` unless you have a reason. It is not about safety
theatre — a constant tells the reader *this will not change*, and the compiler
holds you to it. When you come back in six months, `let` is a fact you can rely
on rather than a promise you have to verify by reading every line below it.

The compiler will even tell you when you got it wrong:

```swift
var count = 5
print(count)
// warning: variable 'count' was never mutated; consider changing to 'let'
```

## A note on where this lives

The examples above sit inside a function, and that is not incidental. A `var`
declared at the top level of a file is a global, and Swift 6 requires globals to
be safe to touch from any task:

```swift
var total = 0    // error: not concurrency-safe because it is
                 // nonisolated global shared mutable state
```

That is a rule about concurrency rather than about `let` and `var` — but it is
why almost all the mutable state you write will be inside a function, a struct,
or an actor.

## What let does not mean

`let` fixes the *binding*, not the contents. For a struct — and most Swift
types are structs — that amounts to the same thing:

```swift
let numbers = [1, 2, 3]
numbers.append(4)   // error: numbers is a 'let' constant
```

But for a class, only the reference is constant:

```swift
class Counter { var value = 0 }

let counter = Counter()
counter.value = 10   // fine: the reference did not change
counter = Counter()  // error: the reference cannot be reassigned
```

That difference between value types and reference types is the thing to keep
hold of. It comes back everywhere.

## Exercise

`name` never changes. Declare it with `let`.
