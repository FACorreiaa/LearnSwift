# Working through Seshat from your own editor

Seshat exposes its lessons and its grader over MCP, so a learner can work
through the course from wherever they already write Swift instead of typing into
a textarea.

The grading is the same grading. A submission that arrives from an editor goes
through the same static validator, the same `swiftc`-to-wasm compile, and the
same assertions as one typed into the browser.

## Setup

Create a token at `/account/tokens`, then paste the command it shows you:

```
claude mcp add --transport http seshat https://your-host/mcp \
  --header "Authorization: Bearer seshat_pat_..."
```

Any MCP client works — the endpoint is streamable HTTP with a bearer token, and
nothing about it is specific to one agent.

The token is shown once. It is stored hashed, so a lost one is replaced rather
than recovered. Revoking is immediate: nothing caches a lookup.

## The tools

| Tool | What it does |
|---|---|
| `list_lessons` | Every lesson by track, with your status for each. Start here — its slugs are what the others take. |
| `get_lesson` | One lesson: markdown body, starter code, the requirements the grader checks, and the limits of the runtime it compiles against. |
| `submit_solution` | Compiles and runs a complete Swift file against a lesson, and records the attempt. |
| `get_progress` | Completed counts per track, and the next lesson to pick up. |

Two things are deliberately absent.

**Solutions are not available through this server.** Not because they are secret
— this repository is public — but because a tool that returns the answer
alongside the question is a tool an agent will answer with. The same goes for
hints.

**Nothing here is streamed.** A verdict is one object, so the transport is
stateless and returns plain JSON.

## What gets recorded

Every submission that arrives this way is stored with a provenance of `agent`
and counted as **assisted**.

That is not a judgement, and it is not a guess. Seshat cannot tell whether you
reasoned the answer out and dictated it or whether the model wrote it while you
watched, and it does not try to. What it knows for certain is which door the
submission came through, so that is what it writes down.

The [leaderboard](/leaderboard) keeps the two counts separate:

- **Solo** — lessons whose first passing attempt was typed in the browser, with
  the solution never revealed.
- **Assisted** — everything else, including every submission from this endpoint.

Only the *first* passing attempt for a lesson decides its label. Solving
something unaided and later pasting the same answer back does not undo it; and
pasting first does not earn a solo count by retyping afterwards.

Neither number is a qualification. Nobody is watching you type, there is no
identity check, and the only person a misreported number costs is you.

## Getting the most out of it

Ask your agent to coach rather than to solve. The tool descriptions ask for
this too, but a description is a request and not a constraint — an agent can
write correct Swift for these lessons whether or not it is asked to. The dual
count is what keeps the record honest; it is not a way of stopping anything.

Read `runtime_limits` before writing code. The `embedded` runtime has no
concurrency runtime and no Unicode tables: no `async`/`await`, no `Task`, no
actors, no string sorting, and no top-level `var`. An agent that does not know
this writes async Swift, reads the error, and writes more async Swift.

## Limits

Submissions are capped at 20 per ten minutes per learner, tighter than the
browser's 90 — an agent does not pause to read a compiler error before trying
again, and every cache miss starts a container.

Resubmitting identical code is free and uncounted, so iterating on one file
costs nothing.

When `submit_solution` says to wait, wait. Retrying immediately will not compile
any faster, and the wait it names is in seconds.

## Turning it off

`MCP_ENABLED=false` leaves the endpoint unmounted. It is on by default.
