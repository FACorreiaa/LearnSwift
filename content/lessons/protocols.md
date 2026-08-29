---
title: Learn Protocols in 4 Minutes
track: patterns
order: 1
minutes: 4
summary: Describe what something can do, then give it a default.
runtime: embedded
starter: |
  struct Dog {
      let name: String
  }

  // Define a `Named` protocol requiring a `name`, and conform Dog to it.
  let dog = Dog(name: "Rex")
  print(dog.name)
solution: |
  protocol Named {
      var name: String { get }
  }

  struct Dog: Named {
      let name: String
  }

  let dog = Dog(name: "Rex")
  print(dog.name)
assertions:
  must_declare:
    - "protocol Named"
    - "Dog: Named"
  output_contains:
    - "Rex"
---

## Concept

A protocol is a list of requirements:

```swift
protocol Named {
    var name: String { get }
    func greet() -> String
}
```

`{ get }` means readable; `{ get set }` means readable and writable. A type
conforms by satisfying everything:

```swift
struct Dog: Named {
    let name: String
    func greet() -> String { "Woof, I'm \(name)" }
}
```

A `let` satisfies `{ get }`. It would not satisfy `{ get set }`.

## Extensions give defaults

This is where protocols in Swift stop resembling interfaces elsewhere. An
extension can supply an implementation, so conforming types get it for free:

```swift
extension Named {
    func greet() -> String { "Hello, I'm \(name)" }
}

struct Cat: Named {
    let name: String
    // greet() comes for free
}
```

Now the protocol carries behaviour, not just shape. This is the "protocol
oriented" part: build up capability in extensions, and let types opt in.

## Constrained extensions

Defaults can be narrowed to types that meet further conditions:

```swift
extension Named where Self: Equatable {
    func isSame(as other: Self) -> Bool { self == other }
}
```

And you can extend a standard-library protocol, which is how `[Int]` gets
methods that `[String]` does not:

```swift
extension Collection where Element == Int {
    var total: Int { reduce(0, +) }
}

print([1, 2, 3].total)   // 6
```

## Composition

```swift
func show(_ value: some Named & Equatable) { }
```

Small protocols combined at the point of use, rather than one large one that
every type must satisfy in full.

## Exercise

Define a `Named` protocol requiring a `name`, and conform `Dog` to it.
