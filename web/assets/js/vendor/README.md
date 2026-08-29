# Vendored JavaScript

Third-party libraries are committed here rather than loaded from a CDN, because the
production binary embeds its assets (`web/assets/assets.go`). The application keeps
working with no third-party origin to trust, block, or outlive us — and a visitor's
browser makes no request we did not put in the repository ourselves.

**Every file here must be self-contained.** No `import` may point at another origin.
A bundle that reaches out to a CDN at runtime defeats the entire reason this directory
exists. Check before committing:

```
grep -oE 'from"[^".][^"]*"' web/assets/js/vendor/<file> | grep -v 'from"\.'
```

Minified bundles produce occasional false positives on that grep (a string literal
ending in `from"` followed by an operator). Read the match before acting on it.

## What is here

| File | Package | Version | License | Source |
|---|---|---|---|---|
| `htmx.min.js` | htmx.org | 2.0.7 | Zero-Clause BSD | copied from north-web-app 2026-08-22 |
| `htmx-ext-sse.js` | htmx-ext-sse | **unrecorded** | Zero-Clause BSD | copied from north-web-app 2026-08-22 |
| `alpine.min.js` | alpinejs | 3.15.0 | MIT | copied from north-web-app 2026-08-22 |
| `three.module.min.js` | three | 0.180.0 (r180) | MIT | `https://cdn.jsdelivr.net/npm/three@0.180.0/build/three.module.min.js` |
| `three.core.min.js` | three | 0.180.0 (r180) | MIT | `https://cdn.jsdelivr.net/npm/three@0.180.0/build/three.core.min.js` |

The htmx and Alpine versions are inherited from North's table, which leaves
`htmx-ext-sse` unrecorded. Treat that one's version as unknown until someone diffs
it against a release — it matters before Phase 2, which streams compile results
over SSE.

**The two three.js files are a pair and must be upgraded together.** From r165 or
so the build is split, and `three.module.min.js` does
`import ... from "./three.core.min.js"`. That import is relative, so it stays within
our own origin and satisfies the rule above — but it does mean the module file alone
is not usable, and mixing versions of the two fails in ways that look like scene
bugs rather than load errors. Both were verified to contain their `@license` header
and to make no cross-origin import; `r180` is read from the `const t="180"` revision
constant in the core bundle, not inferred.

## Adding a file

1. Pin an exact version. Never `@latest`, never a range.
2. Prefer a single-file ESM build. `https://cdn.jsdelivr.net/npm/<pkg>@<version>/+esm`
   bundles a package's ESM entry into one self-contained file.
3. **Check the license header survived.** jsdelivr's `+esm` transform strips comments,
   including `@license` blocks. Restore the header from the package's own dist build.
4. Name it `<package>.module.min.js`, or `<package>-<submodule>.module.js` for a piece
   of a larger library.
5. Run the self-contained check at the top of this file.
6. Add a row to the table. A vendored file with no recorded provenance cannot be
   audited or safely upgraded.
