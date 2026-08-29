---
title: Learn Control Flow in 4 Minutes
track: swift-basics
order: 4
minutes: 4
summary: if, for, and the switch that has to be exhaustive.
runtime: embedded
starter: |
  let score = 72

  // Rewrite this chain as a switch over ranges.
  if score >= 90 {
      print("A")
  } else if score >= 70 {
      print("B")
  } else {
      print("C")
  }
solution: |
  let score = 72

  switch score {
  case 90...:
      print("A")
  case 70..<90:
      print("B")
  default:
      print("C")
  }
assertions:
  must_declare:
    - "switch score"
  output_contains:
    - "B"
---

## Concept

`if` needs no parentheses, and always needs braces:

```swift
if score >= 70 {
    print("pass")
} else {
    print("fail")
}
```

Loops walk a sequence rather than an index:

```swift
for number in 1...3 {
    print(number)          // 1, 2, 3
}

for name in ["a", "b"] {
    print(name)
}
```

`1...3` includes 3; `1..<3` stops before it. That second form is what you want
when counting to a `count`.

## switch is the interesting one

Swift's `switch` must be **exhaustive** — every possible value handled, or the
code does not compile:

```swift
let direction = "north"

switch direction {
case "north": print("up")
case "south": print("down")
}
// error: switch must be exhaustive
```

Add a `default`, or handle every case. That is a nuisance for a `String` and a
genuine feature for an `enum`: add a case to the enum later, and the compiler
lists every switch that now needs updating, instead of letting one fall through
silently at runtime.

There is no implicit fallthrough, so no `break` is needed:

```swift
switch score {
case 90...:      print("A")     // 90 and above
case 70..<90:    print("B")
case let other:  print("below 70: \(other)")
}
```

Cases can match ranges, tuples, and bind values — which is why a `switch` in
Swift replaces chains that would be `if`/`else if` elsewhere.

## Exercise

Rewrite the `if`/`else if` chain as a `switch` over ranges.
