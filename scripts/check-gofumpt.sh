# Ask gofumpt which files are unformatted, rather than formatting them and
# diffing against the index.
#
# The diff approach (which North uses) works in CI, where the checkout is
# clean, but locally it fails on *any* unstaged Go edit and blames formatting
# for it — which sends you to `task fmt`, which changes nothing, which is a
# genuinely confusing five minutes. `gofumpt -l` answers the real question and
# behaves identically in both places.
unformatted=$(go tool gofumpt -l . \
  | grep -vE '(_templ\.go|web/shared/ui/icon/icon_(data|defs)\.go)$' || true)
if [ -n "$unformatted" ]; then
  echo "These files need formatting (run: task fmt):" >&2
  echo "$unformatted" >&2
  exit 1
fi
echo "OK: gofumpt is clean"
