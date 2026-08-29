# Seshat

**Write Swift like a scribe.**

Read the concept in 30 seconds. Try it in 10. Move on.

Seshat is an interactive Swift and SwiftUI learning app: short lessons, each
with an exercise you run in the browser. Named for the Egyptian goddess of
writing, knowledge and architecture — Mistress of the House of Books.

## Stack

Go · [templ](https://templ.guide) · htmx · Alpine · Tailwind v4 · Postgres.
The same stack as North, deliberately: shared conventions, shared operational
model, one mental model across both.

Swift itself is compiled server-side to WebAssembly and executed in the
visitor's browser. There is no in-browser Swift compiler — the toolchain runs
on the host — but because the compiled program runs in the browser's own wasm
sandbox, the server never executes untrusted code. It only ever runs `swiftc`
over untrusted input.

## Getting started

Requires Go 1.27, [Task](https://taskfile.dev), Docker, and the
[Tailwind standalone binary](https://github.com/tailwindlabs/tailwindcss/releases)
v4.3.3 on `PATH`.

```sh
cp .env.example .env
task dev:up          # Postgres + MinIO, then the app with hot reload
```

Then open http://localhost:8090.

`task dev:up` starts Postgres on port **5442**, not 5432, so this stack runs
alongside North's without either having to be stopped.

## Common tasks

| Command | What it does |
|---|---|
| `task dev` | Hot reload, assuming the database is already up |
| `task check` | Everything CI runs — run this before pushing |
| `task fmt` | gofumpt + `templ fmt` |
| `task test` | Test suite (database tests skip when no database is configured) |
| `task migrate:new -- add_x` | Create a migration |
| `task db:reset` | Destroy and recreate local data |

## A note on generated files

`*_templ.go` and `web/assets/css/output.css` are **generated and not
committed**. `web/assets` embeds the `css` directory, so a build with no
stylesheet fails at compile time rather than quietly serving an unstyled page.
Run `task assets` (or `task build`) before `go build` or `go test` in a fresh
checkout. `scripts/check-no-build-artifacts.sh` fails CI if one is ever
committed.

## Layout

```
cmd/web/            entrypoint; `main migrate` applies the schema
internal/           vertical slices: <slice>/{handler,service,repository,db}
internal/shared/    middleware, errors, htmx, database
migrations/         goose SQL, embedded
content/lessons/    lesson markdown with frontmatter
web/                templ pages, mirroring internal/
web/shared/ui/      templUI components, vendored
web/assets/js/vendor/  third-party JS, vendored — never a CDN
```
