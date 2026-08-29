---
title: Learn TaskGroup in 5 Minutes
track: concurrency
order: 4
minutes: 5
summary: Concurrency when the number of jobs is only known at runtime.
runtime: full
starter: |
  func double(_ n: Int) async -> Int { n * 2 }

  func run() async {
      // Double all of these concurrently and sum the results.
      let numbers = [1, 2, 3, 4]
      var total = 0
      for n in numbers {
          total += await double(n)
      }
      print(total)
  }

  await run()
hint: >-
  `withTaskGroup(of:)` gives you a group to `addTask` into, then yields
  results as they finish. Loop over the group with `for await`, adding each
  value up as it arrives.
solution: |
  func double(_ n: Int) async -> Int { n * 2 }

  func run() async {
      let numbers = [1, 2, 3, 4]

      let total = await withTaskGroup(of: Int.self) { group in
          for n in numbers {
              group.addTask { await double(n) }
          }

          var sum = 0
          for await value in group {
              sum += value
          }
          return sum
      }

      print(total)
  }

  await run()
assertions:
  must_declare:
    - "withTaskGroup"
    - "group.addTask"
  output_contains:
    - "20"
---

## Concept

`async let` works when you know how many jobs there are. When the count comes
from a collection, use a task group:

```swift
let total = await withTaskGroup(of: Int.self) { group in
    for n in numbers {
        group.addTask { await double(n) }
    }

    var sum = 0
    for await value in group {
        sum += value
    }
    return sum
}
```

`of: Int.self` is what each child returns. `addTask` starts one immediately.
`for await ... in group` collects results.

## Results arrive out of order

This catches people. The group yields values **as they finish**, not in the
order they were added:

```swift
for await value in group {
    // whichever finished first
}
```

Summing does not care. Building an array does:

```swift
// Wrong: order is not the input order
var results: [Int] = []
for await value in group { results.append(value) }
```

If you need the order, send the index along and reorder afterwards:

```swift
await withTaskGroup(of: (Int, Int).self) { group in
    for (i, n) in numbers.enumerated() {
        group.addTask { (i, await double(n)) }
    }

    var results = Array(repeating: 0, count: numbers.count)
    for await (i, value) in group {
        results[i] = value
    }
    return results
}
```

## Throwing groups

`withThrowingTaskGroup` is the same thing where children can throw. The first
error cancels the remaining children and propagates — which is usually exactly
what you want, and is structured concurrency doing the tidying for you.

## Bounding the width

A group with ten thousand children starts ten thousand tasks. When that matters,
add work as results come in rather than all at once, so only a fixed number are
ever in flight.

## Exercise

Double all four numbers concurrently with a task group, and print the sum.
