---
title: Learn Generics in 4 Minutes
track: patterns
order: 2
minutes: 4
summary: One implementation, many types, checked at compile time.
runtime: embedded
starter: |
  // Write this once, for any type, instead of once per type.
  func firstInt(_ items: [Int]) -> Int? {
      items.first
  }

  print(firstInt([3, 1, 2]) ?? 0)
hint: >-
  Put the placeholder in angle brackets after the function name, then use it
  everywhere a concrete type would go — including the return, which is
  optional because the array may be empty.
solution: |
  func first<T>(_ items: [T]) -> T? {
      items.first
  }

  print(first([3, 1, 2]) ?? 0)
assertions:
  must_declare:
    - "func first<T>"
  output_contains:
    - "3"
---

## Concept

Without generics you write the same function per type:

```swift
func firstInt(_ items: [Int]) -> Int? { items.first }
func firstString(_ items: [String]) -> String? { items.first }
```

With them, once:

```swift
func first<T>(_ items: [T]) -> T? {
    items.first
}
```

`<T>` introduces a placeholder. It is filled in at the call site, at compile
time, and the result is fully type-checked — `first([1, 2])` returns `Int?`,
not `Any?`.

## Constraints

A bare `T` can be passed around but not much else — nothing is known about it.
Constraints add capability:

```swift
func largest<T: Comparable>(_ items: [T]) -> T? {
    items.max()
}
```

`T: Comparable` says "any type that can be ordered", which is what `max()`
needs. Multiple requirements use a `where` clause:

```swift
func describe<T>(_ items: [T]) -> String where T: CustomStringConvertible {
    items.map(\.description).joined(separator: ", ")
}
```

## some and any

Two newer spellings worth telling apart:

```swift
func show(_ value: some Named) { }   // one specific type, known at compile time
func show(_ value: any Named) { }    // any conforming type, decided at runtime
```

`some` is a generic parameter with nicer syntax — the compiler knows the exact
type and can optimise accordingly. `any` is a box that can hold different types
over its lifetime, at the cost of an indirection.

Reach for `some` by default; use `any` when you genuinely need a heterogeneous
collection:

```swift
let things: [any Named] = [dog, cat]
```

## Generic types

```swift
struct Stack<Element> {
    private var items: [Element] = []

    mutating func push(_ item: Element) { items.append(item) }
    mutating func pop() -> Element? { items.popLast() }
}

var numbers = Stack<Int>()
numbers.push(1)
```

`Array`, `Dictionary`, `Optional` and `Result` are all exactly this.

## Exercise

Rewrite `firstInt` as a generic `first` that works for any element type.
