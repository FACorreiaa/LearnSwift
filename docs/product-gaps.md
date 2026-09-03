# Seshat — what's missing before "feature ready"

Snapshot: 2026-08-29. Excludes a mobile client (explicitly out of scope for this list).

**Tier 0 items 0.1 and 0.4–0.6 are now done** (2026-08-29). See `docs/backlog.md` for what shipped.

This document is the *why*. For the addressable, one-at-a-time version — stable IDs, files, acceptance criteria — see **`docs/backlog.md`**.

## What already exists

- 21 lessons across 4 tracks — `swift-basics` (6), `swiftui` (5), `concurrency` (6), `patterns` (4)
- Server-side `swiftc` → WebAssembly, executed in a wasm sandbox; compile cache; clean diagnostics
- Static assertions (`must_declare` / `must_not_use`) plus output assertions (`output_contains` / `output_equals`)
- Email+password auth, server sessions, CSRF, session sweep, login/register rate limits
- Per-lesson progress, completion, attempt history
- Guest mode: reading a lesson *and* checking an answer both work signed-out. This is the single best product decision in the repo — do not regress it.
- 139 tests, 11 packages, CI gate, lesson-solution verification script

Read: the *learning core* is done. What is missing is everything around it — acquisition surface, retention loop, account lifecycle, and the operational guards that let it face the open internet.

---

## Tier 0 — Blockers. Cannot launch publicly without these.

### 0.1 No rate limit on the compile endpoint
`POST /lessons/{slug}/check` is unauthenticated and unmetered. `internal/shared/ratelimit` is wired into login and register only (`internal/auth/handler.go:52`). Every anonymous check spawns a `swiftc` invocation — the most expensive operation in the system.

A single scripted client saturates the compiler host. This is a cost bomb and a trivial DoS. The htmx config in `web/shared/layout/base.templ:23` already swaps 429 into "the quota panel", and `internal/shared/errors/errors.go:26` already defines `ErrRateLimited` — the contract was designed, the enforcement was never wired.

Needs: per-IP limiter on `/check`, a global concurrency cap on the compiler pool, and the quota panel the layout already promises. Guests get a generous but finite budget; signed-in users get more. That asymmetry is also the honest reason to make an account.

### 0.2 No account recovery
No email sending anywhere in the codebase. No password reset, no verification, no change-email. A learner who forgets their password loses their entire history permanently and has no path back except a new account.

For a product whose retention *is* accumulated progress, this converts every forgotten password into permanent churn.

Needs: transactional email (Resend/Postmark), password reset tokens, and — deliberately — *no* mandatory verification at signup. Verify lazily, on first reset.

### 0.3 No legal or data-subject surface
No privacy policy, no terms, no account deletion, no data export. Operating from the EU with named accounts and stored submissions makes deletion and export legal obligations, not features.

Needs: `/privacy`, `/terms`, a settings page with **Delete account** (cascading), and an export.

### 0.4 The landing page is a stub
`web/landing/page.templ` is 64 lines: a mascot, a tagline (*"Write Swift like a scribe."*), a subtitle and two buttons. It is a well-made hero and nothing else — no claim about what makes this different, no demonstration, no track list, and no mention of the fact that no account is needed. Every channel in the marketing plan terminates here.

### 0.5 No metadata, no social preview, no SEO primitives
`base.templ` head has charset, viewport, htmx config, fonts, stylesheet — and nothing else. No `<meta name="description">`, no Open Graph, no Twitter card, no canonical, no `sitemap.xml`, no `robots.txt`.

Consequence: every link shared to X, Slack, Reddit, or Discord renders as a bare grey box. Google has no per-lesson description to show. The lessons are already public and crawlable — the compounding SEO asset is *built* and currently *invisible*.

### 0.6 No analytics
Nothing instrumented. There is no way to answer "did anyone finish a lesson this week", which is the only number that matters under the strangers-count rule. PostHog is already available in this environment.

Instrument exactly six events, no more: `lesson_viewed`, `check_submitted`, `check_passed`, `lesson_completed`, `signed_up`, `returned_day_2`.

---

## Tier 1 — Feature-ready. The product feels incomplete without these.

### 1.1 Failure teaches nothing
On a wrong answer the learner gets `Failures` and raw `Diagnostics`. No hint, no explanation, no escape hatch. `Solution` exists in the lesson model and is deliberately never sent to the browser (`internal/lessons/lesson/lesson.go`) — correct as a default, wrong as an absolute.

A learner stuck on attempt four with no hint and no solution quits. That is the highest-leverage retention fix in the product.

Needs: a `hint` frontmatter field per lesson; hint offered after failure 2; solution revealed after failure 4 or on explicit request, with the attempt marked as "solved with solution" rather than silently passed.

### 1.2 No return path
No dashboard, no "continue where you left off", no streak, no completion state anywhere except the lesson list. A returning learner lands on the landing page and has to remember where they stopped.

Needs: a signed-in home that is a single **Continue: <lesson>** button plus track progress bars. Streaks optional and low priority — the continue button does most of the work.

### 1.3 No review or recall
Every lesson is a one-shot. Nothing brings a concept back. A platform whose claim is *learning* that never revisits anything teaches recognition, not recall.

Needs: a lightweight "review" queue — completed lessons resurface as a bare exercise with no prose after ~7 days. Cheap to build on the existing attempt table; strongly differentiating.

### 1.4 The curriculum has holes
21 lessons is a demo, not a course. `patterns` has 4. There is no capstone, nothing multi-file, nothing that produces a thing the learner keeps.

Missing subjects a Swift learner will notice immediately: error handling / `throws` / `Result`, enums with associated values, `Codable`, extensions, ARC and reference semantics, `map`/`filter`/`reduce`, `Sendable` explicitly, `AsyncSequence`, testing with Swift Testing.

Target ~45 lessons for "complete beginner path", plus one capstone per track.

### 1.5 No in-product feedback channel
When a lesson is wrong, ambiguous, or its assertions reject a valid answer, the learner has no way to say so and silently leaves. At current scale this is the highest-value data stream available.

Needs: one "this lesson is wrong" link per lesson, posting slug + submitted code to a table. Nothing more.

### 1.6 Account settings do not exist
No change password, no change email, no sign-out-everywhere. `DeleteSessionsForUser` exists in the auth queries and is not exposed anywhere.

### 1.7 SwiftUI lessons can only be checked statically, never seen
5 lessons teach a UI framework that renders nothing. `RuntimeNone` is an honest constraint — SwiftUI has no Linux/wasm build and never will. But teaching layout with no visual output is teaching sculpture over the telephone.

Needs: hand-authored before/after screenshots or SVG diagrams per SwiftUI lesson. Content work, not engineering. Cheap, and it fixes the weakest track.

---

## Tier 2 — After the first 10 strangers. Not before.

- **Payments.** No pricing, no Stripe, no tiers. Correct as-is. Building checkout before anyone has finished three lessons is polishing supply while demand is zero.
- **Public profile / shareable result cards.** Real growth loop, worthless with no users to share.
- **OAuth (GitHub/Apple sign-in).** Reduces signup friction, but guest mode already removes the wall that matters.
- **Search across lessons.** Matters at 45+ lessons, not 21.
- **Accessibility audit.** Keyboard-only path through the editor, focus management on htmx swaps, contrast pass. Do it before it becomes 45 lessons of debt.
- **i18n.** Not now.
- **MCP endpoint / public compile API.** Already anticipated in `cmd/web/main.go` comments. A genuine differentiator later ("Swift compilation as an API"), a distraction now.
- **Gamification + AI-assisted learning.** Streaks, XP, spaced repetition, exercise variety, a path map; an LLM error explainer, adaptive hints, tutor. Designed in `docs/gamification.md` (SG-29…SG-51); every item carries an unlock trigger so it is started by data, not by how much it looks missing.

---

## Suggested order

1. Rate limit `/check` + compiler concurrency cap **(0.1)**
2. Landing page + metadata + OG + sitemap **(0.4, 0.5)**
3. Analytics, six events **(0.6)**
4. Hints and solution reveal **(1.1)**
5. Continue button **(1.2)**
6. Ship to strangers → **stop and read the data**
7. Password reset + legal + delete account **(0.2, 0.3)** — before the *second* wave, not the first
8. Curriculum to ~45 lessons **(1.4)**, feedback link **(1.5)**, review queue **(1.3)**

Items 1–5 are roughly two weeks. Everything in Tier 2 waits for evidence.
