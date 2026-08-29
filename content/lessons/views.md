---
title: Learn SwiftUI Views in 3 Minutes
track: swiftui
order: 1
minutes: 3
summary: A view is a struct that describes what to show, not a thing you mutate.
runtime: none
starter: |
  // Make this a SwiftUI view: conform to View and give it a body.
  struct GreetingView {
      let name = "World"
  }
hint: >-
  A view is a struct conforming to `View`, and the one thing that protocol
  requires is a computed `body`. Its type is `some View`, not a concrete
  one.
solution: |
  struct GreetingView: View {
      let name = "World"

      var body: some View {
          Text("Hello, \(name)!")
      }
  }
assertions:
  must_declare:
    - ": View"
    - "var body: some View"
---

## Concept

A SwiftUI view is a `struct` that conforms to `View` and has one required
property: `body`.

```swift
struct GreetingView: View {
    var body: some View {
        Text("Hello, World!")
    }
}
```

That is the whole contract. `body` describes what should be on screen *right
now*, given the view's current values.

## Views are descriptions, not objects

This is the shift from UIKit. You never hold onto a view and change it:

```swift
// UIKit thinking — there is no equivalent of this
label.text = "Hello"
```

Instead, SwiftUI throws the struct away and asks for `body` again whenever
something it depends on changes. Your job is to make `body` a pure function of
the view's data. That is why views are structs and not classes: they are cheap
to create, discard, and recreate thousands of times.

## some View

```swift
var body: some View
```

`some View` means "one specific type conforming to `View`, and the compiler
knows which — you do not have to write it down." The real type of a stacked
layout is something like `VStack<TupleView<(Text, Text)>>`, which nobody wants
to type.

The word to notice is *specific*: the type is fixed at compile time. That is
what lets SwiftUI compare the old and new descriptions efficiently.

## Composing

Views nest, and a custom view is used exactly like a built-in one:

```swift
struct ProfileView: View {
    var body: some View {
        VStack {
            GreetingView()
            Text("Welcome back")
        }
    }
}
```

Small views are the idiom here, not a style preference — they are the unit
SwiftUI re-evaluates.

## Exercise

Make `GreetingView` a real SwiftUI view: conform to `View` and add a `body`
showing `Text`.
