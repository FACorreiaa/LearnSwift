---
title: Learn Structured Concurrency in 5 Minutes
track: concurrency
order: 3
minutes: 5
summary: async let, and why child work cannot outlive its parent.
runtime: full
starter: |
  func fetchGreeting() async -> String { "Hello" }
  func fetchName() async -> String { "World" }

  func run() async {
      // These two do not depend on each other. Start both before waiting.
      let greeting = await fetchGreeting()
      let name = await fetchName()
      print("\(greeting), \(name)!")
  }

  await run()
hint: >-
  `async let` starts the work immediately and hands you a promise of the
  result. Start both bindings first, then await them where the values are
  used — awaiting each one as you declare it makes them sequential again.
solution: |
  func fetchGreeting() async -> String { "Hello" }
  func fetchName() async -> String { "World" }

  func run() async {
      async let greeting = fetchGreeting()
      async let name = fetchName()
      print("\(await greeting), \(await name)!")
  }

  await run()
assertions:
  must_declare:
    - "async let"
  output_contains:
    - "Hello, World!"
---

## Concept

Two awaits in a row happen in order. If each takes a second, that is two
seconds — even when neither depends on the other:

```swift
let greeting = await fetchGreeting()   // 1s
let name = await fetchName()           // 1s, starts after the first finishes
```

`async let` starts the work immediately and waits only where the value is used:

```swift
async let greeting = fetchGreeting()   // starts now
async let name = fetchName()           // starts now, alongside
print("\(await greeting), \(await name)!")   // ~1s total
```

The `await` moves to the point of use. Nothing else changes.

## What "structured" means

Child tasks are bound to the scope that created them. A function cannot return
while work it started is still running — the scope waits, whether you wrote an
`await` or not:

```swift
func run() async {
    async let a = work()
    // Even with no `await a`, this function will not return until a finishes.
}
```

Three consequences, and they are the reason the model exists:

- **No leaks.** Work cannot outlive the thing that started it.
- **Cancellation flows down.** Cancel the parent and every child is cancelled.
- **Errors flow up.** A child that throws propagates to the parent scope.

Compare an unstructured `Task { }`, which is off on its own: nobody waits for
it, cancelling its creator does not cancel it, and its errors go nowhere.

## When it does not fit

`async let` needs the number of jobs known at compile time. For a collection —
one job per item, count decided at runtime — you need a task group, which is
the next lesson.

## Exercise

The two fetches do not depend on each other. Start both before waiting on
either.
