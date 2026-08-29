---
title: Learn async/await in 5 Minutes
track: concurrency
order: 1
minutes: 5
summary: Write code that waits without blocking, and reads top to bottom.
# embedded Swift has no _Concurrency module, so async/await needs the full standard library.
runtime: full
starter: |
  func fetchName() async -> String {
      return "World"
  }

  // Call fetchName and print "Hello, World!"
solution: |
  func fetchName() async -> String {
      return "World"
  }

  let name = await fetchName()
  print("Hello, \(name)!")
assertions:
  output_contains:
    - "Hello, World!"
  must_declare:
    - "await"
---

## Concept

`async` marks a function that might need to wait. `await` marks the point where
the waiting happens.

```swift
func fetchName() async -> String {
    try? await Task.sleep(for: .seconds(1))
    return "World"
}

let name = await fetchName()
print("Hello, \(name)")
```

That reads top to bottom, like ordinary code — which is the entire point.

## What await actually does

It does **not** block the thread. At an `await`, your function suspends and
hands the thread back; when the result is ready, it resumes.

This is why `await` is a keyword you can see rather than something the runtime
hides. Every `await` is a place where other work can run, and where the world
may have changed by the time you continue.

```swift
let count = items.count
await save()
// items.count may no longer equal count here
```

## Doing two things at once

`await` in sequence waits in sequence. Two seconds, total:

```swift
let a = await fetchName()
let b = await fetchName()
```

`async let` starts both immediately and waits at the point of use. One second:

```swift
async let a = fetchName()
async let b = fetchName()
let both = await (a, b)
```

## Where async starts

`await` is only legal inside an async context. From synchronous code, `Task`
opens one:

```swift
Task {
    let name = await fetchName()
    print(name)
}
```

## Exercise

Call `fetchName()` and print `Hello, World!`.
