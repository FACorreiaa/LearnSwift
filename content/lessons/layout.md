---
title: Learn Stacks in 4 Minutes
track: swiftui
order: 3
minutes: 4
summary: VStack, HStack, ZStack, and the Spacer that pushes things apart.
runtime: none
starter: |
  // Put these side by side with the price pushed to the trailing edge.
  Text("Total")
  Text("£42")
solution: |
  HStack {
      Text("Total")
      Spacer()
      Text("£42")
  }
assertions:
  must_declare:
    - "HStack"
    - "Spacer()"
---

## Concept

Three stacks, three axes:

```swift
VStack { }   // children top to bottom
HStack { }   // children leading to trailing
ZStack { }   // children back to front, overlapping
```

```swift
VStack(alignment: .leading, spacing: 8) {
    Text("Fernando")
        .font(.headline)
    Text("Lisbon")
        .foregroundStyle(.secondary)
}
```

`alignment` runs across the stack's axis — a `VStack` aligns horizontally, an
`HStack` vertically. That reads backwards at first and then stops being
confusing.

## Spacer

`Spacer()` expands to fill whatever space is going, which is how you push
things apart:

```swift
HStack {
    Text("Total")
    Spacer()
    Text("£42")     // pushed to the trailing edge
}
```

Two spacers centre something:

```swift
HStack {
    Spacer()
    Text("Centred")
    Spacer()
}
```

## ZStack for layering

```swift
ZStack {
    Color.blue
    Text("On top")
        .foregroundStyle(.white)
}
```

Later children draw on top. `alignment` on a `ZStack` positions children
relative to each other, which is how badges and overlays are built.

## How layout is actually decided

SwiftUI's layout is a conversation, not a command:

1. The parent **offers** a size to the child.
2. The child **chooses** its own size.
3. The parent **places** it.

The child always picks. A parent cannot force a size on it — which is why
`.frame()` works by inserting a new view that makes a different offer, rather
than by resizing anything.

## Exercise

Put the two labels side by side, with the price pushed to the trailing edge.
