---
title: Learn Lists in 4 Minutes
track: swiftui
order: 5
minutes: 4
summary: Showing a collection, and what Identifiable is really for.
runtime: none
starter: |
  let names = ["Ada", "Grace", "Alan"]

  // Show these in a List. `names` holds Strings, which are not Identifiable —
  // tell List what to use as the identity.
  List {
  }
hint: >-
  `List` needs to tell rows apart. When the elements are plain strings with
  no id of their own, the value itself is the identity.
solution: |
  let names = ["Ada", "Grace", "Alan"]

  List(names, id: \.self) { name in
      Text(name)
  }
assertions:
  must_declare:
    - "List("
    - "id:"
---

## Concept

`List` shows a collection, one row per element:

```swift
struct Person: Identifiable {
    let id = UUID()
    let name: String
}

List(people) { person in
    Text(person.name)
}
```

`ForEach` does the same inside another container when you do not want the list
chrome:

```swift
VStack {
    ForEach(people) { person in
        Text(person.name)
    }
}
```

## Identity is the whole point

`List` and `ForEach` need to tell rows apart between updates. Not to draw them
— to know that *this* row is the same row it was a moment ago, so it can animate
a move rather than a delete and an insert, and so it can keep the right row's
text field focused.

For a type conforming to `Identifiable`, that is automatic. For anything else,
say what identifies it:

```swift
List(names, id: \.self) { name in
    Text(name)
}
```

`\.self` means "the value is its own identity", which is fine for a list of
distinct strings and wrong the moment there are duplicates — two identical
strings become one identity, and the rows misbehave in ways that look like a
rendering bug.

Prefer a real identifier when there is one:

```swift
List(people, id: \.email) { person in
    Text(person.name)
}
```

## Sections

```swift
List {
    Section("Team") {
        ForEach(people) { Text($0.name) }
    }
    Section("Archived") {
        ForEach(archived) { Text($0.name) }
    }
}
```

## Exercise

Show `names` in a `List`. Strings are not `Identifiable`, so tell `List` what
to use as identity.
