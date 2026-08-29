---
title: Learn Property Wrappers in 4 Minutes
track: patterns
order: 3
minutes: 4
summary: Reusable behaviour attached to a property, and what @State really is.
runtime: embedded
starter: |
  // Make this a property wrapper so @Clamped can be attached to a property.
  struct Clamped {
      var wrappedValue: Int
  }

  var volume = 11
  print(volume)
hint: >-
  The wrapper needs `wrappedValue` and an `init(wrappedValue:)`, and both
  have to clamp. Setting a property runs the setter, so the ceiling belongs
  there too, not only at initialisation.
solution: |
  @propertyWrapper
  struct Clamped {
      private var value: Int

      init(wrappedValue: Int) {
          value = min(max(wrappedValue, 0), 10)
      }

      var wrappedValue: Int {
          get { value }
          set { value = min(max(newValue, 0), 10) }
      }
  }

  struct Speaker {
      @Clamped var volume = 0
  }

  func demo() {
      var speaker = Speaker()
      speaker.volume = 11
      print(speaker.volume)
  }

  demo()
assertions:
  must_declare:
    - "@propertyWrapper"
  output_contains:
    - "10"
---

## Concept

A property wrapper factors out behaviour that would otherwise be repeated in
every property's getter and setter:

```swift
@propertyWrapper
struct Clamped {
    private var value: Int

    init(wrappedValue: Int) {
        value = min(max(wrappedValue, 0), 10)
    }

    var wrappedValue: Int {
        get { value }
        set { value = min(max(newValue, 0), 10) }
    }
}
```

The `init(wrappedValue:)` is not optional decoration: writing
`@Clamped var volume = 0` *calls* it, and without one the assignment does not
compile. It is also the only place the initial value passes through the
clamping, so leaving it out would let `@Clamped var volume = 99` through.

Attach it with `@`:

```swift
struct Speaker {
    @Clamped var volume = 0
}

var speaker = Speaker()
speaker.volume = 11
print(speaker.volume)   // 10 — clamped on the way in
```

`wrappedValue` is the only requirement. Reading and writing the property goes
through it.

## What the compiler does

`@Clamped var volume = 0` becomes roughly:

```swift
private var _volume = Clamped(wrappedValue: 0)
var volume: Int {
    get { _volume.wrappedValue }
    set { _volume.wrappedValue = newValue }
}
```

That is the whole trick. Once you can see the expansion, wrappers stop being
mysterious — the storage is a separate value, and the property is a computed
window onto it.

## The projected value

`$` exposes something else the wrapper chooses to publish:

```swift
@propertyWrapper
struct Clamped {
    var wrappedValue: Int
    var projectedValue: Bool { wrappedValue == 10 }
}

print(speaker.$volume)   // true when at the maximum
```

This is exactly what SwiftUI's `$` is. `@State var count` gives `count` (the
`Int`) and `$count` (a `Binding<Int>`) — nothing special about SwiftUI, just a
projected value with a well-chosen type.

## The ones you already use

`@State`, `@Binding`, `@Observable`, `@Environment` are property wrappers.
Knowing the mechanism means the rule "`@State` owns, `@Binding` borrows" stops
being something to memorise: they are different wrappers over different
storage, projecting different things.

## Exercise

Make `Clamped` a property wrapper, attach it, and show a value of 11 clamped
to 10.
