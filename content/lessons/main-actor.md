---
title: Learn MainActor in 4 Minutes
track: concurrency
order: 6
minutes: 4
summary: The one actor with a thread, and how to get onto it.
runtime: full
starter: |
  func loadTitle() async -> String { "Loaded" }

  // Updating the interface must happen on the main actor. Annotate this.
  final class ViewModel {
      var title = ""

      func load() async {
          title = await loadTitle()
      }
  }

  let model = ViewModel()
  await model.load()
  print(model.title)
hint: >-
  Annotate the whole type rather than each method. Everything it touches is
  then on the main actor, which is why reaching it from outside needs
  `await`.
solution: |
  func loadTitle() async -> String { "Loaded" }

  @MainActor
  final class ViewModel {
      var title = ""

      func load() async {
          title = await loadTitle()
      }
  }

  let model = await ViewModel()
  await model.load()
  print(await model.title)
assertions:
  must_declare:
    - "@MainActor"
  output_contains:
    - "Loaded"
---

## Concept

`@MainActor` is a global actor bound to the main thread. Anything annotated with
it runs there:

```swift
@MainActor
final class ViewModel {
    var title = ""
}
```

Interface work must happen on the main thread. Before Swift Concurrency that
was a convention enforced by crashes; now it is enforced by the compiler.

## Three places to put it

On a type, so everything in it is main-actor bound:

```swift
@MainActor
final class ViewModel { }
```

On a single member, when only part of a type needs it:

```swift
class Loader {
    @MainActor
    func updateUI() { }
}
```

Or on a closure, to hop onto the main actor for a moment:

```swift
await MainActor.run {
    label.text = "Done"
}
```

## What it does not do

`@MainActor` does not mean "runs immediately" and it does not mean the whole
function occupies the main thread. An `await` inside a main-actor function
still suspends, and the main thread gets on with other work while it does.

```swift
@MainActor
func load() async {
    let data = await fetch()   // main thread is free here
    title = data               // back on the main actor
}
```

That is the useful shape: expensive work suspends, the interface stays
responsive, and the assignment lands back on the main actor without you
scheduling anything.

## Where it comes for free

SwiftUI's `View.body` is already `@MainActor`, as is `@Observable` state that
drives it. Most of the time the annotation is only needed on model types that
outlive a single view.

## Exercise

Updating the interface must happen on the main actor. Annotate `ViewModel`.
