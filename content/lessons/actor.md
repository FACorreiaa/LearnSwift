---
title: Learn Actors in 5 Minutes
track: concurrency
order: 5
minutes: 5
summary: A type that protects its own state, and the reentrancy that surprises people.
runtime: full
starter: |
  // Two tasks incrementing this at once is a data race. Make it an actor.
  final class Counter {
      private var value = 0

      func increment() {
          value += 1
      }

      func current() -> Int {
          value
      }
  }

  let counter = Counter()
  counter.increment()
  print(counter.current())
solution: |
  actor Counter {
      private var value = 0

      func increment() {
          value += 1
      }

      func current() -> Int {
          value
      }
  }

  let counter = Counter()
  await counter.increment()
  print(await counter.current())
assertions:
  must_declare:
    - "actor Counter"
  output_contains:
    - "1"
---

## Concept

An `actor` is a reference type that guarantees only one task touches its
mutable state at a time:

```swift
actor Counter {
    private var value = 0

    func increment() {
        value += 1
    }
}
```

No locks written by hand, and no way to forget one. The compiler enforces it:
reaching in from outside requires `await`.

```swift
let counter = Counter()
await counter.increment()
let now = await counter.current()
```

That `await` is the queue. Calls from different tasks take turns.

## Inside is synchronous

Within the actor, its own state is just state — no `await`, no ceremony:

```swift
actor Counter {
    private var value = 0

    func incrementTwice() {
        value += 1
        value += 1      // no await; already isolated
    }
}
```

## Reentrancy is the trap

Actors are **reentrant**. While one call is suspended at an `await`, another
call can start. The actor is not locked for the duration of your function — only
between suspension points.

```swift
actor Balance {
    private var amount = 0

    func withdraw(_ n: Int) async {
        guard amount >= n else { return }
        await audit()              // suspends — another call can run here
        amount -= n                // `amount` may have changed
    }
}
```

The check happened before the suspension; the mutation happens after. Between
them, anything could have run.

The rule that follows: **do not assume state survives an `await`.** Re-check
after suspending, or gather what you need before it.

```swift
func withdraw(_ n: Int) async {
    await audit()
    guard amount >= n else { return }   // check after suspending
    amount -= n
}
```

This is not a flaw. Non-reentrant actors deadlock; reentrancy is the price of
not deadlocking, and it is paid in vigilance around `await`.

## Exercise

Two tasks incrementing this class at once is a data race. Make it an `actor`.
