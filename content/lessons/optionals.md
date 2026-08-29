---
title: Learn Optionals in 3 Minutes
track: swift-basics
order: 3
minutes: 3
summary: A box that might hold a value — or might hold nothing.
# structs, Optionals and interpolation all work under embedded Swift: 38 KB instead of 1.9 MB.
runtime: embedded
starter: |
  let greeting: String? = "World"

  // Print "Hello, World!" — but only if greeting has a value.
solution: |
  let greeting: String? = "World"

  if let greeting {
      print("Hello, \(greeting)!")
  }
assertions:
  output_contains:
    - "Hello, World!"
  must_not_use:
    - "!"
---

## Concept

An `Optional` is a box. It either holds a value, or it holds `nil`.

```swift
var name: String? = "Fernando"   // a box with something in it
var empty: String? = nil         // a box with nothing in it
```

The `?` is the whole idea: `String` always has a string. `String?` might not.
Swift will not let you use the contents until you have opened the box and
checked.

## Unwrap safely

`if let` opens the box and gives you the value only when there is one:

```swift
if let name {
    print("Hello, \(name)")   // runs only when name is not nil
}
```

Since Swift 5.7 you can write `if let name` instead of `if let name = name`.

For the opposite shape — leave early when the box is empty — use `guard let`:

```swift
func greet(_ name: String?) {
    guard let name else { return }
    print("Hello, \(name)")
}
```

## The one to avoid

`!` force-unwraps: it opens the box and *insists* something is there.

```swift
print(name!)   // crashes at runtime if name is nil
```

It is not a shortcut, it is a promise — and the compiler stops checking once you
make it. Every force-unwrap is a crash waiting for the one input you did not
think of.

## Exercise

Make it print `Hello, World!` without using `!`.
