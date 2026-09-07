# Seshat — blocker & gap backlog

Companion to `docs/product-gaps.md` (the *why*), `docs/marketing-plan.md` (the *when*) and `docs/gamification.md` (the *later* — retention mechanics and AI-assisted learning, SG-29…SG-51, all parked behind the gate).
This file is the *what* — one addressable item at a time, in execution order.

Snapshot: 2026-08-29. IDs are stable; do not renumber when items close.

**SG-01 through SG-06 are done** — shipped 2026-08-29, full `task check` green across 13 packages. The ship gate is now the next item. See "Shipped" at the bottom for what landed and what it changed.

## Board

| # | ID | Item | Sev | Effort | Blocks |
|---|---|---|---|---|---|
| 1 | SG-01 | ~~Rate limit `/check` + compiler concurrency cap~~ | ✅ done | M | — |
| 2 | SG-02 | ~~Real landing page~~ | ✅ done | M | — |
| 3 | SG-03 | ~~Page metadata, OG, sitemap, robots~~ | ✅ done | S | — |
| 4 | SG-04 | ~~Analytics — six events~~ | ✅ done | S | — |
| 5 | SG-05 | ~~Hints + solution reveal~~ | ✅ done | M | — |
| 6 | SG-06 | ~~Signed-in home / continue button~~ | ✅ done | S | — |
| — | — | **▸ SHIP GATE — you are here. Stop, get 10 strangers, read data** | — | — | — |
| — | SG-25 | ~~MCP endpoint~~ | ✅ done | L | see note below |
| — | SG-51 | ~~Submission provenance~~ | ✅ done | S | with SG-25 |
| — | SG-37 | ~~Dual-track leaderboard~~ | ✅ done | M | with SG-25 |
| 7 | SG-07 | Transactional email + password reset | 🔴 blocker | M | 2nd wave |
| 8 | SG-08 | Legal pages, account deletion, data export | 🔴 blocker | M | EU compliance |
| 9 | SG-09 | Account settings page | 🟡 med | S | — |
| 10 | SG-10 | In-product lesson feedback link | 🟠 high | S | content quality |
| 11 | SG-11 | Curriculum to ~45 lessons | 🟠 high | L | "complete" claim |
| 12 | SG-12 | SwiftUI lesson visuals | 🟡 med | M | weakest track |
| 13 | SG-13 | Review / recall queue | 🟡 med | M | learning claim |
| 14 | SG-14 | Accessibility pass | 🟡 med | M | debt growth |

Parked until ≥10 strangers: SG-20…SG-26 at the bottom.

**SG-25, SG-51 and SG-37 were built ahead of this gate, on 2026-09-07, by a
deliberate decision rather than by drift.** The strangers count was still 0. The
argument for it was that the MCP endpoint aims at a different audience — the
Swift 6 migration developers in `docs/marketing-plan.md`, who have Macs and run
agents — and that provenance had to land with it or the endpoint would quietly
make the progress numbers untrue. The argument against it is the one in
`docs/marketing-plan.md` §9, and it is a good one: two surfaces before anybody
has used one is how a solo project spreads itself thin. Recorded here so the
decision is legible later, whichever way it turns out.

Notably, this route needed none of the AI blockers in `docs/gamification.md`:
the learner brings their own agent, so there is no LLM client, no token spend,
no prompt-injection surface, and neither SG-40 nor the SG-49 eval harness
applies. SG-29 is likewise not a prerequisite — MCP callers are always
authenticated.

Effort: S ≈ half a day · M ≈ 1–3 days · L ≈ weeks.

---

## SG-01 — Rate limit `/check` + compiler concurrency cap ✅

**Status:** done, 2026-08-29 · **Severity:** 🔴 blocker (security + cost) · **Effort:** M

### State before the fix
`internal/lessons/handler.go:58`

```go
r.Post("/lessons/{slug}/check", h.check)
```

Unauthenticated, unmetered. Every call reaches `Executor.Run` → `compiler.Compile`, which spawns a container running `swiftc` — the most expensive operation in the system. `internal/shared/ratelimit` is instantiated only for login and register (`internal/auth/handler.go:52-53`). There is no cap on concurrent compiles anywhere: `internal/compiler/compiler.go:120` `Compile` has no admission control.

The contract for the fix is already written and unimplemented:
- `internal/shared/errors/errors.go:26` — `ErrRateLimited` "renders as 429 into the quota panel"
- `web/shared/layout/base.templ:23` — htmx config already swaps 429 into the page
- **Neither the limiter call nor the quota panel exists.**

### Risk
One scripted client saturates the compiler host. Front-page HN traffic does it accidentally. This is both a denial-of-service vector and an unbounded infrastructure cost.

### Work
1. Per-IP limiter on `/check`. Reuse `ratelimit.New(limit, window)` — the existing `Allow`/`RetryAfter`/`Reset` API needs no change.
   - Guest budget: ~20 checks / 10 min per IP.
   - Signed-in budget: ~60 checks / 10 min per user ID. Key on user ID when present, IP otherwise — this asymmetry is also the honest argument for making an account.
   - Note the package doc: in-memory, per-process, resets on deploy. Acceptable here for the same reasons stated for sign-in; revisit if replicas > 1.
2. Global concurrency cap on compiles. Buffered-channel semaphore sized from a new `config.MaxConcurrentCompiles` (default ~4, tune to host cores). Place it in `internal/executor` around `Run`, not inside `compiler.Compile`, so cache hits are never gated.
3. Return `apperr.ErrRateLimited` → 429 with a `Retry-After` header.
4. Build the quota panel fragment in `web/lesson/exercise.templ` — the thing the layout has been promising since day one. Copy should name the number and the reset time, not just refuse.
5. Cache lookup happens *before* the limiter charge. A repeat of an already-compiled submission costs nothing and should not be billed against the budget.

### Acceptance
- 21st guest check inside the window returns 429 and swaps a readable quota panel
- Cached submissions do not consume budget
- Concurrent compiles never exceed the configured cap under a load test
- Tests: limiter keying (IP vs user), 429 body, cache-before-limit ordering, semaphore ceiling

---

## SG-02 — Real landing page ✅

**Status:** done, 2026-08-29 · **Severity:** 🔴 blocker · **Effort:** M

### State before the fix
`web/landing/page.templ` — 64 lines, renders one line of visible copy: *"Write Swift like a scribe."* Every channel in the marketing plan terminates here.

### Work
Above the fold, in order:
1. **The claim**, per the marketing plan wedge: Swift that runs in your browser. No Mac. No Xcode. No download.
2. **A live embedded exercise** — a real one, working, on the page. The product demos itself in ten seconds; describing it instead is strictly worse. Reuse the `web/lesson/exercise.templ` component against a chosen intro lesson.
3. **"No account needed"** stated explicitly. Guest mode already ships this (`handler.go:55-58`); it is the strongest sentence available and is currently unsaid.
4. Track list with lesson counts — `swift-basics` 6, `swiftui` 5, `concurrency` 6, `patterns` 4.
5. One line on how it works, linking the technical write-up: server-side `swiftc` → WebAssembly, executed in the browser's own sandbox, server never runs untrusted code.

### Acceptance
- A stranger can run Swift from the landing page without clicking through or signing up
- The claim is legible in under five seconds
- The embedded exercise shares the real check path (no special-cased demo route)

---

## SG-03 — Page metadata, OG, sitemap, robots ✅

**Status:** done, 2026-08-29 · **Severity:** 🔴 blocker · **Effort:** S

### State before the fix
`web/shared/layout/base.templ:10-30`. The head carries charset, viewport, htmx config, font preloads, title, stylesheet, two scripts. That is all. Missing: `<meta name="description">`, Open Graph, Twitter card, canonical, `sitemap.xml`, `robots.txt`.

Lessons are already public and crawlable (`LoadUser`, not `RequireAuth` — `cmd/web/main.go:211-214`). The compounding SEO asset is **built and invisible**.

### Consequence
Every link shared to X, Slack, Reddit or Discord renders as a bare grey box. Google has no per-lesson description to show. This lands the day before any launch post and costs half a day.

### Work
1. Widen `Base(title string)` to take a small `Meta` struct — `Title`, `Description`, `Canonical`, `Image` — rather than growing the parameter list. Every call site updates; there are few.
2. Per-lesson description: the lesson `Summary` field already exists on `lesson.Lesson` and is written for exactly this. Wire it through.
3. OG + Twitter card tags. One default share image in `web/assets/brand/`; per-lesson images are a later nicety, not now.
4. `sitemap.xml` generated from `lessons.Index` at startup — the index is already in memory, so this is a handler, not a build step.
5. `robots.txt` allowing everything, pointing at the sitemap.
6. `<link rel="canonical">` on lesson pages.

### Acceptance
- Every page emits a description; lesson pages emit their own `Summary`
- A lesson URL pasted into Slack/X renders a card with title, description, image
- `/sitemap.xml` lists all 21 lessons and validates
- `/robots.txt` references the sitemap

---

## SG-04 — Analytics: six events ✅

**Status:** done, 2026-08-29 · **Severity:** 🔴 blocker · **Effort:** S

### State before the fix
Nothing instrumented. There is no way to answer *"did anyone finish a lesson this week"* — the only number that matters under the strangers-count rule.

### Work
Instrument exactly six events. Not seven.

| Event | Fired at |
|---|---|
| `lesson_viewed` | `lessons.Handler.show` |
| `check_submitted` | `lessons.Handler.check`, on entry |
| `check_passed` | `check`, on pass |
| `lesson_completed` | `lessons.Handler.complete` |
| `signed_up` | `auth.Service.Register` success |
| `returned_day_2` | derived, not emitted |

Properties: lesson slug, track, runtime, signed-in bool, attempt ordinal. No submitted source code — it is user content and belongs in the feedback table (SG-10), not the analytics pipeline.

PostHog is already available in the environment. Server-side capture, not a client script — it survives ad blockers, which developer audiences run near-universally.

### Acceptance
- Funnel visible: viewed → submitted → passed → completed
- Guest vs signed-in segmentable
- No PII and no source code in event payloads

---

## SG-05 — Hints and solution reveal ✅

**Status:** done, 2026-08-29 · **Severity:** 🟠 high · **Effort:** M

### State before the fix
On a wrong answer the learner receives `result.Failures` and raw `result.Diagnostics` (`internal/lessons/handler.go:199-200`). No hint, no explanation, no escape.

`lesson.Lesson.Solution` exists and is deliberately never sent to the browser:

> *"Solution is never sent to the browser. It exists so a test can prove the assertions actually accept a correct answer."* — `internal/lessons/lesson/lesson.go`

Correct as a default. Wrong as an absolute. A learner stuck on attempt four with no hint and no way out quits, and that is the modal churn event in every learning product.

### Work
1. Add `hint` to lesson frontmatter and to `lesson.Lesson`. Author one per exercise — 21 lines of content work.
2. Track per-session failure count for a slug. Guests: a cookie or the existing session. Signed-in: `progress.Attempts` already counts (`internal/progress/progress.go:126`).
3. After failure **2** — offer the hint, revealed on click.
4. After failure **4** — offer "show me the answer". On reveal, mark the attempt `solved_with_solution` rather than passing it silently; the learner's history should stay honest.
5. Diagnostics already run through `CleanDiagnostics`. Good. Do not regress it.

### Acceptance
- Hint appears after the second failure, never before
- Solution reachable after the fourth failure and never earlier
- Revealed solutions are distinguishable from earned passes in `progress`
- Hint text never reaches the DOM before it is unlocked (server-gated, not CSS-hidden)

---

## SG-06 — Signed-in home / continue button ✅

**Status:** done, 2026-08-29 · **Severity:** 🟠 high · **Effort:** S

### State before the fix
`cmd/web/main.go:219` — `GET /` renders `landing.Page()` unconditionally, signed in or not. There is no dashboard. A returning learner lands on a marketing page and must remember where they stopped.

Everything needed already exists: `progress.Service.ForUser` returns the whole map (`internal/progress/progress.go:94`), and `lessonpages.View` already computes `IsCompleted` and `CompletedCount` per track (`web/lesson/pages.templ:26-38`).

### Work
1. When a user is present on `GET /`, render an app home instead of the landing page.
2. The page is essentially one thing: **Continue: `<lesson title>`** — the first incomplete lesson in track order. Everything else is secondary.
3. Under it, per-track progress bars reusing `CompletedCount`.
4. Streaks: skip. The continue button does most of the work and streaks punish the exact returning learner they are meant to attract.

### Acceptance
- Signed-in `GET /` shows continue + progress, never the marketing page
- Continue resolves to the first incomplete lesson in order
- All tracks complete → a finished state, not an empty box

---

## ▸ SHIP GATE — next stop after SG-05 and SG-06

After SG-06: **stop building.** Ship to strangers. Target 10, defined as people neither known personally nor prompted twice. Watch sessions, read every failed submission, fix lessons that trip people up.

Everything below is decided by what those ten do — not by what looks missing from here.

---

## SG-07 — Transactional email + password reset

**Severity:** 🔴 blocker (before the *second* wave) · **Effort:** M

### Current state
No email anywhere in the codebase. `internal/auth` has Register / Login / Logout and nothing else. No reset, no verification, no change-email.

A learner who forgets their password loses their entire history permanently, with no path back but a new account. For a product whose retention *is* accumulated progress, that converts every forgotten password into permanent churn.

### Work
1. Provider: Resend or Postmark. One `internal/shared/mail` package behind a small interface so tests use a recorder, not a network.
2. Reset tokens: single-use, ~1 h expiry, hashed at rest — mirror the existing session-token handling in `internal/auth/session.go`. New migration.
3. Routes: `GET/POST /forgot-password`, `GET/POST /reset-password`.
4. Rate limit both — `ratelimit` is already in this handler.
5. Reset response must not disclose whether an address is registered. Same copy either way, per the `ErrNotFound` reasoning already stated in `apperr`.
6. On successful reset, call the existing `DeleteSessionsForUser` query — currently defined (`internal/auth/db/queries.sql:44`) and unused anywhere.
7. **No mandatory verification at signup.** Verify lazily, on first reset. A verification wall in front of a product whose pitch is "no account needed to start" is self-defeating.

### Acceptance
- Full reset round-trip works
- Tokens single-use and expiring
- Reset invalidates all existing sessions
- Response identical for registered and unregistered addresses
- Reset endpoints rate limited

---

## SG-08 — Legal pages, account deletion, data export

**Severity:** 🔴 blocker (EU) · **Effort:** M

### Current state
No privacy policy, no terms, no delete-account, no export. Operating from the EU with named accounts and stored code submissions makes deletion and export obligations, not features.

Stored personal data today: `users` (email, password hash), `sessions`, `user_lessons`, and **attempt rows containing submitted source code** (`internal/progress/db/queries.sql:32`).

### Work
1. `/privacy` and `/terms` as static templ pages. Name every category above, plus the analytics from SG-04 and the email provider from SG-07.
2. `DELETE /account`, confirmation-gated. Cascading delete across users → sessions → user_lessons → attempts. Verify the FK cascades in the migrations actually cover attempts.
3. `GET /account/export` → JSON of profile, progress, attempts. A handler over existing queries; no new storage.
4. State the compile-pipeline data flow explicitly: submitted code is sent to a container running `swiftc` and cached by content hash (`executor.CacheKey`). Someone will ask, and the honest answer is a good one.

### Acceptance
- Deletion removes every row across all four tables, verified by test
- Export contains everything the deletion removes
- Both linked from account settings (SG-09) and from the footer

---

## SG-09 — Account settings page

**Severity:** 🟡 med · **Effort:** S · **Depends on:** SG-07, SG-08

### Current state
No settings page. No change password, no change email, no sign-out-everywhere. `DeleteSessionsForUser` exists in the queries and is exposed nowhere.

### Work
`/account` with: change password (requires current), change email, **sign out everywhere** (wire the existing query), export (SG-08), delete account (SG-08).

### Acceptance
- Password change requires the current password and rotates sessions
- Sign-out-everywhere invalidates every session including the current one
- Destructive actions confirmation-gated

---

## SG-10 — In-product lesson feedback link

**Severity:** 🟠 high · **Effort:** S · **Best data-per-hour in the file.**

### Current state
When a lesson is wrong, ambiguous, or its assertions reject a valid answer, the learner has no way to say so. They leave silently. At current scale this is the most valuable stream available and it costs almost nothing to open.

### Work
1. One "something's wrong with this lesson" link per lesson page.
2. `POST /lessons/{slug}/feedback` storing slug, optional message, the last submitted code, timestamp, user ID if present. New table, new migration.
3. Rate limit it (SG-01's limiter serves).
4. No admin UI. Read it with SQL. Building a dashboard for a table with four rows is the exact supply-side polish to avoid.

### Acceptance
- One click to report from any lesson
- Last submission captured automatically — the learner should not have to paste anything
- Works signed-out

---

## SG-11 — Curriculum to ~45 lessons

**Severity:** 🟠 high · **Effort:** L · **After the ship gate. Not before.**

### Current state
21 lessons: `swift-basics` 6, `swiftui` 5, `concurrency` 6, `patterns` 4. A demo, not a course. No capstone, nothing multi-file, nothing the learner keeps.

### Missing subjects a Swift learner notices immediately
Error handling — `throws` / `try` / `Result`; enums with associated values; pattern matching beyond basic `switch`; `Codable`; extensions and protocol extensions; ARC, value vs reference semantics; `map` / `filter` / `reduce`; `Sendable` taught explicitly rather than encountered as an error; `AsyncSequence` / `AsyncStream`; testing with Swift Testing.

### Work
1. Fill `swift-basics` to ~15 and `patterns` to ~10 first — `patterns` at 4 is the thinnest track.
2. One capstone per track: multi-concept, still single-file, produces something the learner keeps.
3. Every new lesson passes `scripts/check-lessons.sh` before merge — it already gates all 16 executables and runs in ~88s.
4. Mind the runtime split when authoring: `RuntimeEmbedded` has no concurrency runtime and no Unicode normalization, so no `async`/`await` and no string sorting. Swift 6 strict concurrency also rejects top-level `var` there — the failure that broke three lessons on 2026-08-28. Declare `runtime: full` when in doubt and accept the ~1.9 MB artifact.

### Acceptance
- ~45 lessons, no track under 8
- Every executable lesson green under `check-lessons.sh`
- One capstone per track

---

## SG-12 — SwiftUI lesson visuals

**Severity:** 🟡 med · **Effort:** M

### Current state
Five lessons teach a UI framework that renders nothing. `RuntimeNone` is an honest constraint and permanent — SwiftUI is closed-source Apple, with no Linux or WebAssembly build, as `lesson.go` already states plainly. Static assertions (`must_declare: "@State"`) still work and are genuinely useful.

But teaching layout with no visual output is teaching sculpture over the telephone. This is the weakest track and the one a visiting iOS developer will judge hardest.

### Work
Hand-authored before/after screenshots or SVG diagrams per SwiftUI lesson, showing what the code produces. Content work, not engineering. Cheapest large improvement available.

Optionally: an "open in Swift Playgrounds" link for readers who *do* have Apple hardware. Honest about the limit rather than papering over it.

### Acceptance
- Every SwiftUI lesson shows what its code renders
- The constraint is stated to the learner, not hidden

---

## SG-13 — Review / recall queue

**Severity:** 🟡 med · **Effort:** M

### Current state
Every lesson is one-shot. Nothing brings a concept back. A platform claiming to teach that never revisits anything trains recognition, not recall.

### Work
1. Completed lessons resurface after ~7 days as a bare exercise — starter code, no prose.
2. Surface from the signed-in home (SG-06): "3 lessons to review".
3. Build on `user_lessons.completed_at` plus the attempts table. No spaced-repetition algorithm — a fixed 7/30 day interval is enough and is honest about what it does.

### Acceptance
- Reviews appear 7 days after completion
- Review attempts recorded distinctly from first passes
- Skippable without penalty

---

## SG-14 — Accessibility pass

**Severity:** 🟡 med · **Effort:** M · **Do before 45 lessons, not after.**

### Current state
Never audited. The custom editor in `web/assets/js/editor.js` is the main risk: it was written dependency-free after `@codemirror/lang-swift` turned out not to exist on any CDN, so it carries none of an editor library's built-in a11y work.

Doing this at 21 lessons is a day. At 45 lessons and three more page types it is a week.

### Work
1. Keyboard-only path: land in the editor, type, submit via Ctrl+Enter, read the result, move to the next lesson — without a mouse.
2. Focus management on htmx swaps. Result panels swap in via 422/429 and must move focus, or a screen reader user never learns the answer arrived.
3. `aria-live` on the result panel.
4. Contrast pass across both themes — there is a theme switcher, so both need checking.
5. Verify `prefers-reduced-motion` is respected.

### Acceptance
- Full lesson loop completable by keyboard alone
- Result panels announced by a screen reader
- Both themes pass WCAG AA contrast

---

## Parked — not before 10 strangers

Listed so they stop occupying attention, not so they get built.

| ID | Item | Why parked |
|---|---|---|
| SG-20 | Payments, pricing, Stripe | Free until 100 weekly actives. "Free, no signup" is the strongest launch sentence available; monetizing now would price the wrong thing. |
| SG-21 | Public profiles, shareable result cards | Real growth loop, worthless with nobody to share to. |
| SG-22 | OAuth (GitHub / Apple) | Reduces signup friction — but guest mode already removed the wall that mattered. |
| SG-23 | Lesson search | Matters at 45+ lessons (SG-11), not 21. |
| SG-24 | i18n | Not now. |
| SG-25 | ~~MCP endpoint~~ / public compile API | **MCP endpoint shipped 2026-09-07** — `internal/mcp`, four tools over stateless streamable HTTP, bearer-authenticated with the access tokens in `internal/auth/token.go`. See `docs/mcp.md`. The *public compile API* half remains parked: it is a second product with its own abuse surface. |
| SG-26 | Mobile client | Explicitly out of scope. Fix responsive web instead — it serves the same need at 5% of the cost. |
| SG-29 | Persist guest attempts | Designed in `docs/gamification.md`. Not parked in spirit: guest failures currently leave no row (`exercise_attempt.user_id` is NOT NULL), so "read every failed submission" is impossible for the anonymous strangers the gate is about. Schema-only, no UI. Candidate to do at the gate. |
| SG-30…SG-36, SG-38, SG-39 | Duolingo-style mechanics — streaks, XP, spaced repetition, exercise kinds, path map, achievements, placement, reminders, mascot | Designed in `docs/gamification.md` with per-item unlock triggers. Retention mechanics with nobody to retain. |
| SG-37 | ~~Leagues /~~ leaderboards | **Shipped 2026-09-07** as a dual-track board — `internal/leaderboard`, opt-in, Solo and Assisted counted separately. Built early because provenance made the distinction possible and an MCP endpoint without it would have made "solved 40 lessons" meaningless. Leagues and tiers remain parked. |
| SG-40…SG-50 | AI-assisted learning — provider-agnostic LLM layer, error explainer, adaptive hints, code review, tutor, practice variants, failure clustering, adaptive path, search, eval harness, in-editor assist | Designed in `docs/gamification.md`. The eval harness (SG-49) gates every learner-facing AI item; none ships without a golden set. Unaffected by SG-25: an endpoint the learner points their own agent at spends no tokens and writes no prompts. |
| SG-51 | ~~Submission provenance~~ | **Shipped 2026-09-07**, and stronger than designed. The spec had a browser-side paste heuristic only; the MCP channel makes `agent` a fact rather than an inference. Migration `20260907120000_submission_provenance.sql`. Still labels and never refuses. |

---

## Notes for whoever picks this up cold

- **Guest mode is load-bearing.** Reading a lesson *and* checking an answer both work signed-out, deliberately (`internal/lessons/handler.go:49-58`). It is the best decision in the repo and the strongest thing every launch post has to say. Do not regress it; SG-01 in particular must throttle guests, never block them.
- **Generated files are not committed.** `*_templ.go` and `web/assets/css/output.css`. Run `task assets` before `go build` in a fresh checkout; `scripts/check-no-build-artifacts.sh` fails CI if one is ever committed.
- **`task check` for the inner loop; `task check:all` includes the lesson gate** (~88s, needs the 4 GB `seshat-compiler:6.3.1` image).
- **OrbStack, not Docker Desktop** — socket at `~/.orbstack/run/docker.sock`. Executor tests skip without it; the auth session tests hang rather than fail fast when Postgres is configured but down.


---

## Shipped

### 2026-08-29 — SG-01 … SG-04

`task check` green: 12 packages, gofumpt clean, `go vet` clean, no tracked generated files.

**SG-01 — the endpoint is metered.**
`POST /lessons/{slug}/check` now charges a per-identity limiter: 30 checks / 10 min for a guest keyed on IP, 90 for a signed-in learner keyed on user id (`internal/lessons/handler.go`). A submission whose module is already compiled is admitted free — `Executor.Cached` was added for exactly that, so a resubmission of unchanged code is never billed for work that costs a hash lookup. Separately, `MAX_CONCURRENT_COMPILES` (default 4) caps concurrent container compiles via a semaphore in `internal/executor`, with a 10s admission wait and a new `executor.ErrBusy` past it. Two new panels — `QuotaPanel` and `BusyPanel` — swap into the result slot on 429, which is the contract the layout's htmx config and `apperr.ErrRateLimited` had been promising since day one. `middleware.ClientIP` now owns the "which address do we count against" rule; `internal/auth` delegates to it rather than keeping a second copy.

**SG-02 — the landing page argues.**
It leads with the wedge from the marketing plan ("Write Swift. Run it. In your browser.") and then proves it: the `variables` exercise is embedded live, using the exported `lessonpages.ExerciseForm` posting to the real grading route — the same component, not a mock. Below it: a three-step explanation of the compile-to-wasm pipeline (the mechanism *is* the differentiator for a developer audience), the track list with counts, and a closing CTA. "No account needed" is now said out loud in two places; it was previously true and unstated.

**SG-03 — links preview and lessons are indexable.**
`layout.Base` takes a `Meta` (title, description, canonical path, image) instead of a bare title string, and emits description, canonical, Open Graph and Twitter card tags. Lesson pages pass their frontmatter `Summary` straight through, so every lesson has a distinct search result and preview card. `/sitemap.xml` is generated from the in-memory index — a lesson added to `content/lessons` is in it on deploy — and `/robots.txt` points at it. `layout.SetBaseURL` is called once from `main`: canonical URLs are deliberately not derived from the Host header, which an attacker sets. With no origin configured the absolute tags are omitted rather than guessed. A 1200×630 preview card ships at `web/assets/brand/og-default.png`, authored as SVG and rasterised by `scripts/build-og.sh`.

**SG-04 — six events, and only six.**
`internal/shared/analytics` wraps PostHog with a non-blocking queue (1024 deep, batched, drops rather than blocks) and a bounded `Close`, so neither a slow endpoint nor a hung one can touch a request or a shutdown. Wired: `lesson_viewed`, `check_submitted`, `check_passed`, `lesson_completed`, `signed_up`. `returned_day_2` stays derived at query time. An empty `POSTHOG_API_KEY` returns the no-op client, so local development needs no credential and no network. Guests get a daily-rotating salted hash of IP + user agent as their distinct id — enough to count one session as one person, deliberately not enough to follow them into next week, and no cookie is set to do it better. A test asserts submitted source code never reaches an event payload.

**Bug found and fixed while verifying (not in the original SG-01…04 scope).**
The "checking is unavailable" panel — the one whose whole purpose is to avoid telling a learner their correct code is wrong because a binary is missing — was rendered at 503, and the layout's htmx config discarded `[45]..`. The panel was built, rendered, and thrown away on every response: a learner hitting an unavailable compiler saw *nothing at all happen*, which is worse than the wrong verdict the panel exists to prevent. Reproduced in a browser against a host missing the `seshat-compiler:6.3.1` image. `503` is now in the swap list beside `422` and `429`, with a regression test in `internal/lessons` and a test in `cmd/web` pinning the whole status-to-swap contract.

**Not verified end-to-end:** a *passing* submission. The `seshat-compiler:6.3.1` image is absent from this machine, so the executor path returns unavailable. The static-validation path was exercised live (30 submissions graded, 31st refused). Run `task compiler:build` before trusting the full pipeline.

**Deliberately not done here:** the featured landing lesson is chosen by slug (`variables`) rather than by position, so reordering the curriculum cannot silently change the front page. If that lesson is ever removed the page logs a warning and renders without the exercise rather than failing to boot.


### 2026-08-29 — SG-05 and SG-06

`task check` green: 13 packages (`web/lesson` now has tests of its own).

**SG-05 — failure teaches something now.**
Every lesson carries a `hint` in its frontmatter — 21 written, one per exercise, each pointing at the shape of the answer without containing a line of it (a test enforces that second part by checking no hint quotes a solution line verbatim). A hint is offered after the **second** failure, the solution after the **fourth**. The first wrong answer gets nothing: being wrong once is working, not being stuck.

The gate is the server, not the template. Neither the hint nor the solution is in the page until a request asks for it — `GET /lessons/{slug}/hint` and `POST /lessons/{slug}/solution`, both refusing with 403 until the attempts are there. A hint hidden behind a CSS class is a hint already given to anyone who opens the source, and a test asserts the text is absent from every response up to the one that grants it.

Counting failures works signed-out too, because guest mode is the whole funnel: a `seshat_tries` cookie holds `slug:count` pairs, capped at 12 lessons and 1 KB, unsigned on purpose — the worst a forged value buys is an earlier hint, and holding a signing key to protect a number that only helps its holder is a poor trade. Signed-in learners are counted from `exercise_attempt` instead. Passing clears the count either way, so returning to revise does not open on an offer of the answer.

New migration `20260829200000_solution_reveals.sql` adds `user_lesson.solution_revealed_at`. A learner who was shown the answer has not solved the exercise, and a history that cannot tell the two apart is worth less than none. Nullable rather than a boolean, because the interesting fact is when they gave up.

**SG-06 — the root knows who is asking.**
`GET /` is now one route that renders the marketing page for a stranger and a dashboard for someone signed in — a redirect would make a returning learner watch the pitch flash past on the way to their own progress. The dashboard is one **Continue** button (the first unfinished lesson in curriculum order, not the most recently touched) plus a `role="meter"` progress bar per track, so the number is announced rather than carried only by a width. Percentages round down: reporting 100% on nineteen of twenty is a small lie that undermines every other number on the page. A learner with no progress is invited to "Start the first lesson" rather than to continue something they never began, and finishing everything gets its own state instead of an empty box. Losing the progress query falls back to the landing page rather than erroring.

**Bug found and fixed while verifying (again outside scope).** The htmx config discarded `503`, so the "checking is unavailable" panel — built precisely so nobody is told their correct code is wrong because a binary is missing — was rendered and thrown away on every response. Reproduced against a host missing the compiler image. Fixed, with a regression test and a `cmd/web` test pinning the whole status-to-swap contract (`422`, `429`, `503`).

**Verified in a browser, as a guest:** two failures → hint appears; two more → solution appears; both arrive by htmx swap without disturbing the code in the editor. The dashboard was verified by rendering it against a fixture rather than by creating an account.

**Still not verified end-to-end:** a *passing* submission. `seshat-compiler:6.3.1` is absent from this machine.
