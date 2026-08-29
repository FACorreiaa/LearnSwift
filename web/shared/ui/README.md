# Vendored UI components

These are [templUI](https://templui.io) components, copied into this repository
by the templUI CLI (see `.templui.json`) rather than imported from the module.
Each file keeps the version stamp the CLI wrote into its header — currently
**v1.12.1**.

templUI is MIT licensed, © Axel Adrian. That license covers these files; the
repository's own `LICENSE` covers everything else.

## Why vendored rather than imported

Vendoring is templUI's own recommended workflow, and it is what makes these
components *ours to edit*: a component that needs a Seshat-specific variant is
changed here, not worked around at the call site.

It also removes the `github.com/templui/templui` module from `go.mod` entirely.
The only thing still importing it was `SetupScriptRoutes` in
`web/shared/utils`, which serves component JavaScript out of the module's
embedded FS — and which is inert in the vendored workflow, because the CLI
rewrites its base path away from `/templui/js`. Component scripts are ordinary
files under `web/assets/js` here, served by the asset handler in `cmd/web`.

## Upgrading

Re-run the templUI CLI. It rewrites imports to the module path in
`.templui.json`, so the result should need no manual fixing — but check that
`web/shared/utils/templui.go` did not regain `SetupScriptRoutes` and a
dependency on the templui module along with it.

The one thing to watch: the `icon` package (`icon_data.go`, `icon_defs.go`) is a
generated Lucide dump of roughly 420 KB. Both `.golangci.yml` and `task fmt`
exclude it, because reformatting a generated file produces a diff nobody wants
to review.
