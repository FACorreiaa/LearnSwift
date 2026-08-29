---
title: Learn @State in 4 Minutes
track: swiftui
order: 4
minutes: 4
summary: How a SwiftUI view remembers anything at all.
# SwiftUI is closed-source and Apple-only: there is no Linux or WebAssembly build, so this
# exercise can only be checked statically.
runtime: none
starter: |
  // A view's body is recomputed constantly, so a plain `var` cannot hold state.
  // Declare `count` so that tapping the button updates the label.

  struct CounterView: View {
      var count = 0

      var body: some View {
          Button("Tapped \(count) times") {
              count += 1
          }
      }
  }
solution: |
  struct CounterView: View {
      @State private var count = 0

      var body: some View {
          Button("Tapped \(count) times") {
              count += 1
          }
      }
  }
assertions:
  must_declare:
    - "@State"
---

## Concept

A SwiftUI view is a **struct**, and SwiftUI throws it away and rebuilds it
whenever it needs to draw. So a plain property cannot remember anything — it is
reset every time, and the struct is immutable anyway:

```swift
struct CounterView: View {
    var count = 0          // reset on every rebuild

    var body: some View {
        Button("Tapped \(count) times") {
            count += 1     // error: self is immutable
        }
    }
}
```

`@State` moves the value *out* of the struct. SwiftUI stores it alongside the
view and hands it back each time the view is rebuilt:

```swift
struct CounterView: View {
    @State private var count = 0

    var body: some View {
        Button("Tapped \(count) times") {
            count += 1
        }
    }
}
```

Now the struct is still thrown away and rebuilt — but the value survives, and
changing it is what *causes* the rebuild.

## Why private

`@State` is the view's own memory. Marking it `private` says so, and stops
anyone passing an initial value from outside and expecting it to stay in sync —
it will not.

To let a *child* view change a value the parent owns, pass a `Binding` instead:

```swift
struct ParentView: View {
    @State private var count = 0

    var body: some View {
        CounterView(count: $count)   // $ makes a Binding
    }
}

struct CounterView: View {
    @Binding var count: Int
    ...
}
```

`@State` owns. `@Binding` borrows.

## The rule of thumb

Use `@State` for a value this view owns, that no one else needs, and that does
not survive the view going away — a toggle, a text field's contents, which tab
is selected. Anything bigger belongs in a model.

## Exercise

Fix `CounterView` so tapping the button updates the label.
