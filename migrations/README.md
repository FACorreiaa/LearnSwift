# Migrations

goose, embedded via `fs.go`, applied by `main migrate` or on boot when
`AUTO_MIGRATE=true`.

## Naming

`YYYYMMDDHHMMSS_description.sql` — **digits only in the timestamp, no `T`
separator.** goose parses the leading number as the version and *silently
skips* a file it cannot parse. A migration named `20260822T101500_x.sql` will
never run, and nothing will say so. `TestEveryMigrationFilenameIsParseable`
in `internal/shared/database` exists to catch exactly that.

Create one with:

```
task migrate:new -- add_exercise_attempts
```

## Rules

- Always write the `-- +goose Down` section. An unreversible migration is
  discovered at the worst possible moment.
- Prefer `text` with a `CHECK` constraint over a Postgres enum. Adding a value
  to an enum is a migration; adding one to a CHECK is a migration you can also
  roll back.
- Migrations run with `WithAllowOutofOrder(true)`, so a branch merged late can
  still apply. Do not rely on version order for correctness.
