---
title: Learn View Modifiers in 3 Minutes
track: swiftui
order: 2
minutes: 3
summary: Every modifier returns a new view, so the order you apply them matters.
runtime: none
starter: |
  // Give the text some padding, then a blue background — in that order, so the
  // colour extends behind the padding.
  Text("Hello, World!")
solution: |
  Text("Hello, World!")
      .padding()
      .background(Color.blue)
assertions:
  must_declare:
    - ".padding()"
    - ".background("
---

## Concept

A modifier is a method that returns a **new** view wrapping the old one:

```swift
Text("Hello")
    .font(.title)
    .foregroundStyle(.blue)
    .padding()
```

Nothing is being mutated. `.padding()` does not add padding to the `Text` — it
returns a new view that contains the `Text` and adds space around it.

## Which is why order matters

Once you see modifiers as wrapping, this stops being surprising:

```swift
Text("Hello")
    .padding()
    .background(Color.blue)   // blue extends behind the padding
```

```swift
Text("Hello")
    .background(Color.blue)   // blue hugs the text
    .padding()                // padding added outside the blue
```

Reading outward: in the first, the background wraps the padded text. In the
second, the padding wraps the coloured text. Same two modifiers, different
picture.

This catches everyone once. When a border, background, or tap target looks
wrong, the order is the first thing to check.

## Common ones

```swift
Text("Hello")
    .font(.headline)
    .foregroundStyle(.secondary)
    .padding(.horizontal, 16)
    .frame(maxWidth: .infinity, alignment: .leading)
```

`.frame` is worth singling out: it does not resize the view so much as place it
inside a box of the given size. `maxWidth: .infinity` means "take the width you
are offered", which is how you get something to fill its parent.

## Exercise

Give the text padding, then a blue background — in that order, so the colour
extends behind the padding.
