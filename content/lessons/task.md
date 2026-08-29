---
title: Learn Task in 4 Minutes
track: concurrency
order: 2
minutes: 4
summary: The bridge from ordinary code into async, and the handle you can cancel.
runtime: full
starter: |
  func fetchName() async -> String { "World" }

  // `await` is only legal in an async context. Open one.
  print("Hello, \(fetchName())!")
solution: |
  func fetchName() async -> String { "World" }

  let task = Task {
      print("Hello, \(await fetchName())!")
  }

  _ = await task.value
assertions:
  must_declare:
    - "Task {"
  output_contains:
    - "Hello, World!"
---

## Concept

`await` is only legal inside an async context. `Task` creates one:

```swift
Task {
    let name = await fetchName()
    print(name)
}
```

That is the bridge: synchronous code on the outside, asynchronous work on the
inside. It is what you reach for in a button action, in `onAppear`, or anywhere
else the caller cannot itself be `async`.

## A Task is a handle

Creating one gives you something you can wait on:

```swift
let task = Task {
    await slowWork()
    return 42
}

let result = await task.value    // waits, and rethrows any error
```

And something you can cancel:

```swift
task.cancel()
```

## Cancellation is cooperative

This is the part worth internalising: `cancel()` does not stop anything. It
sets a flag. Work that never checks the flag runs to completion regardless.

```swift
let task = Task {
    for item in items {
        try Task.checkCancellation()   // throws once cancelled
        await process(item)
    }
}
```

Or check it without throwing:

```swift
if Task.isCancelled { return }
```

Most of the standard library's async work — `Task.sleep`, `URLSession` —
already checks for you, which is why cancellation often appears to work without
any effort. The moment you write a long loop of your own, it is yours to
handle.

## Detached tasks, and why to avoid them

```swift
Task.detached {
    await work()
}
```

A detached task inherits nothing from where it was created: no priority, no
task-local values, no actor context. That sounds like isolation and is usually
just a way to lose the things structured concurrency gives you. Use plain
`Task` unless you can say precisely why you need otherwise.

## Exercise

`await` is only legal in an async context. Open one with `Task`.
