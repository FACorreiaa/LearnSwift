# Seshat — marketing plan

Written 2026-08-29. Horizon: 8 weeks to first 10 strangers, 6 months to first 100 weekly actives.

Companion doc: `docs/product-gaps.md`. The Tier 0 items there are marketing blockers, not engineering preferences — every channel below terminates on a landing page that currently renders one sentence, and every shared link currently previews as a grey box.

---

## 1. The wedge

**Do not launch as "learn Swift".** That market has an incumbent: Paul Hudson's Hacking with Swift is free, ten years deep in SEO, and owns the phrase. Attacking it head-on with 21 lessons is how this dies quietly.

Launch on the one thing Seshat can say that nobody else can:

> **Swift that runs in your browser. No Mac. No Xcode. No download.**

Everyone teaching Swift assumes Apple hardware. Swift Playgrounds is iPad/Mac only. Every tutorial opens with "create a new Xcode project". Seshat compiles `swiftc` server-side to WebAssembly and runs it in the visitor's own browser sandbox — a Windows laptop, a Chromebook, a Linux desktop, a phone on a bus.

That is not a feature. That is the entire positioning, and it is defensible because it took real engineering (Go + SwiftWasm + wazero + a sandboxed compile pipeline) that a content site will not replicate.

### Beachhead audience, in order

**1. Swift-curious developers without a Mac.** Backend and web developers who keep hearing about server-side Swift (Vapor, Hummingbird), or who want to evaluate the language before buying Apple hardware. Currently *completely unserved* — the first step in every existing resource is a hardware purchase. Small segment, zero competition, and they are loud when someone finally serves them.

**2. iOS developers migrating to Swift 6 strict concurrency.** Acute, current, expensive pain. `actor`, `@MainActor`, `Sendable`, structured concurrency, global mutable state errors — six lessons already exist on exactly this. This audience has money and searches error messages at 11pm. Highest monetization potential later.

**3. CS students and bootcampers told to "learn Swift".** Largest segment, lowest intent, worst monetization. Serve them, do not target them.

Target 1 for the story. Target 2 for the traffic. Both point at the same product.

---

## 2. Competitive map

| Who | Their strength | Their gap | Seshat's line |
|---|---|---|---|
| Hacking with Swift | Free, enormous, trusted, top of every SERP | Read-only. Nothing executes, nothing is graded, nothing knows you were there | "Paul teaches it. Seshat makes you type it and tells you if you're right." |
| Swift Playgrounds | Apple-native, polished, free | iPad/Mac only. Locked to Apple hardware | "Same idea, any browser." |
| Kodeco (ex-raywenderlich) | Depth, video, brand | Paid, heavyweight, project-scale, needs Xcode | "Thirty seconds, not thirty minutes." |
| Exercism (Swift track) | Free, exercise-based, human mentors | Local toolchain setup; mentor queue latency | "Zero setup, instant verdict." |
| Boot.dev | The interactive model, executed well | Go/Python/JS. No Swift, no plans for it | The model works. Nobody applied it to Swift. |
| YouTube (Sean Allen, Swiftful Thinking) | Reach, personality | Passive. Nothing runs, nothing checks | Not a competitor — a distribution partner. |

**The one-sentence claim:** *the only place you can write Swift, run it, and be told whether it's right — without owning a Mac.*

Every channel message is a restatement of that sentence.

---

## 3. The engineering story is the launch story

The build is more interesting to developers than the product is. Lead with it.

Compiling Swift to WebAssembly server-side, executing untrusted learner code in the browser's own sandbox so the server never runs it, `wazero` for the host-side path, a 64 MiB module budget, the embedded SDK at ~38 KB versus the full stdlib at ~1.9 MB, and the discovery that Swift 6 strict concurrency rejects top-level `var` under the embedded SDK — that is a genuinely good technical post and it has a working demo attached.

Write it once. It becomes: the Show HN, the Swift Forums thread, the dev.to crosspost, and the answer to "how does this work" for the next two years.

Working title: **"Running Swift in the browser without a Mac: compiling to WebAssembly for a learning app."**

---

## 4. Channels, ranked

### Tier A — do these

**1. Hacker News — Show HN.** Highest single-shot upside. The angle is the compiler pipeline, not the lessons. Post the engineering write-up as a Show HN with the live link. Tuesday–Thursday, ~08:00–10:00 ET. One shot; do not repost. Prerequisites: rate limiting live (an HN front page will otherwise melt the compiler host), landing page real, OG tags present.

**2. Swift Forums (`forums.swift.org`) — SwiftWasm section.** The SwiftWasm working group has very few production consumers of their toolchain. Seshat is one. This is not a launch post, it is a genuine contribution report: what worked, what broke, the embedded-SDK constraints found. Buys credibility with the exact people who amplify Swift tooling, and they are the most likely source of the first ten strangers.

**3. Programmatic SEO on Swift error messages.** The compounding engine, and the reason lessons were made public and crawlable.

Build one page per common Swift 6 diagnostic — `reference to var 'x' is not concurrency-safe because it is non-isolated global shared mutable state`, `main actor-isolated property cannot be referenced from a nonisolated context`, `type 'X' does not conform to the 'Sendable' protocol` — each showing the broken code, the error, the fix, and a **runnable editor with the broken code already loaded**.

Nobody else on the internet can offer "run the fix right here" for a Swift error. These are exact-match searches with near-100% intent, made by people at their most frustrated. Start with 15 pages sourced from the errors already hit building the lessons; grow from real `/check` failure logs once traffic exists. Six-month payoff, so start in week 1.

**4. Reddit — r/swift, r/iOSProgramming, r/SwiftUI.** Genuinely receptive to free tools, allergic to marketing. Post as the maker, lead with the constraint solved ("I got Swift running in the browser so you can learn it without a Mac — free, no signup"), answer every comment, never post the same thing twice. The no-signup fact is what earns upvotes; guest mode already ships it.

**5. Newsletters.** One submission each to **iOS Dev Weekly**, **SwiftLee**, and **Swift Weekly Brief**. Submit the technical write-up, not the product page. Editors link engineering; they ignore launches. Free, one email, potentially thousands of exactly-right readers.

### Tier B — after Tier A produces signal

**6. Short-form video.** The product format — read 30s, try 10s — *is* the short-form format. Screen-record one lesson: concept, broken code, fix, green check. 40 seconds. Post to YouTube Shorts and TikTok. Twenty of these are a week of work and give the entire library a second distribution surface. Only do this once ≥30 lessons exist.

**7. X / Bluesky iOS dev community.** Do not ask for shares. Publish one genuinely useful free artifact — a Swift 6 concurrency migration cheat-sheet, every rule with a runnable example — and let it be cited. Donny Wals, Antoine van der Lee, Majid Jabrayilov, Sean Allen all link good free resources unprompted.

**8. GitHub.** The repo itself is marketing to this audience. A README that opens with the WebAssembly architecture and a screenshot earns stars from people who will never take a lesson but will mention it to someone who would.

### Do not do

- Paid ads. Zero product-market signal, CAC unknowable, budget is better spent as time on lessons.
- Product Hunt. Wrong audience for a developer education tool. Consumer-app dynamics, no lasting SEO.
- Cold outreach at volume. Burns goodwill in a small, tight community that talks to itself.
- A second brand, a rename, or a redesign. Not before ten strangers have used this one.

---

## 5. Eight-week sequence

**Week 0 — unblock the funnel.** Nothing ships publicly until: rate limit on `/check` and a compiler concurrency cap; a real landing page (the claim, a live embedded exercise above the fold, "no account needed", the track list); `<meta description>` + OG + Twitter card + `sitemap.xml` + `robots.txt`; PostHog with six events. Two weeks of work, compressible to one.

**Week 1 — seed by hand, not by broadcast.** Post the SwiftWasm thread on Swift Forums. Publish the technical write-up on the site. Personally hand the link to 20 developers — Swift-curious non-Mac people first. Goal: **10 strangers**, defined as people neither known personally nor prompted twice. Watch every session. Read every failed submission. Change lessons that trip people up.

**Week 2 — fix what week 1 exposed.** Ship hints and solution reveal. Ship the continue button. Expect the drop-off to be exactly where the frustration was, and expect it earlier in the funnel than predicted.

**Week 3 — Show HN.** Only if week 1 produced ten strangers who did not immediately bounce. One shot. Be at the keyboard all day answering comments — that is where the value actually is.

**Week 4 — Reddit and newsletters.** Different framing per subreddit. Newsletter submissions the same week so a linked mention compounds with the traffic.

**Weeks 5–8 — compound.** Ship 15 error-message SEO pages. Grow the curriculum toward 45 lessons. Publish one follow-up technical post. Start short-form video only if lesson count supports it. No new channels.

---

## 6. Metrics

Under the strangers-count rule, one number gates everything:

**Strangers count** — people who used Seshat without being personally asked. Currently **0**. Ten before any structural change (rename, redesign, re-architecture, payments) is even discussed.

Then, in order:

- **North star:** learners completing ≥3 exercises in a week
- Guest → first `check_submitted` rate (measures whether the landing page works)
- `check_submitted` → `check_passed` rate (measures whether lessons are teachable — a low rate is a *content* bug)
- Guest → signup rate (measures whether progress is worth an account)
- Day-2 return rate (measures whether anything here is habit-forming)

Six-month targets: 100 weekly actives, 40% day-2 return, 500 organic monthly visits from error-message pages.

Explicitly not tracked: signups (vanity — guests are the real audience), page views, GitHub stars.

---

## 7. Money — later, deliberately

No pricing page. No Stripe. Not in this plan.

Free everything until 100 weekly actives. The reason is not generosity — it is that the free-and-no-signup fact is the strongest thing to say in every launch post, and monetizing before knowing which track people actually finish would price the wrong thing.

When the time comes, the shape is legible from the audience map: the beginner path stays free forever (it is the SEO and word-of-mouth engine), and the paid product is aimed at audience 2 — a **Swift 6 concurrency migration track** with harder graded exercises drawn from real migration failures. Around €7/month or €49 lifetime. Audience 2 has employers; audience 3 does not.

Revisit at 100 weekly actives. Not before.

---

## 8. Assets to write

In priority order — each is written once and reused across every channel:

1. Landing page copy — the claim, the constraint solved, a live exercise, no signup wall
2. The technical write-up — Swift → WebAssembly, the whole pipeline
3. 15 Swift 6 error-message pages, each with a loaded runnable editor
4. Swift 6 concurrency migration cheat-sheet — free, ungated, linkable
5. Three Reddit posts, framed per subreddit
6. Three newsletter submissions
7. 20 short-form lesson recordings — week 5 onward

---

## 9. The one risk worth naming

Seshat's core loop is genuinely good and genuinely small. The temptation will be to widen the product — more tracks, mobile, an API, certificates — because building is more comfortable than being read by strangers.

The plan above is deliberately ordered so that shipping to strangers happens in **week 1**, with 21 lessons and an incomplete product, and everything after week 1 is decided by what they do rather than by what seems missing. Twenty-one lessons is enough to learn whether anyone wants this. Forty-five lessons built before anyone has tried twenty-one is a different, worse activity wearing the same clothes.
