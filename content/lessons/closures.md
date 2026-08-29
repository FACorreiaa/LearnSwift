---
title: Learn Closures in 5 Minutes
track: swift-basics
order: 6
minutes: 5
summary: A function without a name, shortened until it almost disappears.
# full rather than embedded, at fifty times the artifact size, because embedded
# Swift cannot print an Array — Array's CustomStringConvertible conformance is
# unavailable there. `print(evens)` is what anyone would actually write, and a
# Basics lesson should not teach around a limitation of one build mode.
runtime: full
starter: |
  let numbers = [5, 2, 8, 1]

  // Keep only the even numbers, using filter and a closure.
  print(numbers)
solution: |
  let numbers = [5, 2, 8, 1]

  let evens = numbers.filter { $0 % 2 == 0 }
  print(evens)
assertions:
  must_declare:
    - "filter"
  output_contains:
    - "[2, 8]"
---

## Concept

A closure is a function you write inline. The full form spells everything out:

```swift
let numbers = [5, 2, 8, 1]

let doubled = numbers.map({ (number: Int) -> Int in
    return number * 2
})
```

Swift then lets you delete almost all of it. Each step below is the same code:

```swift
numbers.map({ (number: Int) -> Int in return number * 2 })
numbers.map({ number in return number * 2 })   // types inferred
numbers.map({ number in number * 2 })          // single expression, no return
numbers.map({ $0 * 2 })                        // positional shorthand
numbers.map { $0 * 2 }                         // trailing closure
```

That last form is what you will actually read and write. Knowing it is the
first form with pieces removed is what makes it legible rather than magic.

## The `in` keyword

`in` separates the closure's signature from its body. When you see it, the part
before is parameters and return type; the part after is the work.

```swift
numbers.sorted { left, right in
    left > right
}
```

## Trailing closure syntax

When the closure is the last argument, it can move outside the parentheses —
and if it is the *only* argument, the parentheses go too:

```swift
numbers.filter({ $0 > 3 })   // ordinary
numbers.filter { $0 > 3 }    // trailing
```

This is why so much Swift and SwiftUI reads as `something { ... }`: it is a
function call whose last argument is a closure.

## Capturing

A closure captures the variables it mentions, and keeps them alive:

```swift
func makeCounter() -> () -> Int {
    var count = 0
    return {
        count += 1
        return count
    }
}

let next = makeCounter()
print(next())   // 1
print(next())   // 2
```

`count` outlives the function it was declared in, because the closure still
refers to it. That is the "closure" part of the name.

## Exercise

Use `filter` with a closure to keep only the even numbers, and print them.
