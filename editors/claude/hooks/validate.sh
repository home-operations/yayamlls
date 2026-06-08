#!/usr/bin/env sh
# PostToolUse hook: validate the YAML file Claude just edited and feed any
# error-severity diagnostics back. The payload arrives as JSON on stdin; exit
# code 2 surfaces this script's stderr to Claude, any other code shows nothing.
set -eu

input=$(cat)
file=$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null || true)

case "$file" in
  *.yaml | *.yml) ;;
  *) exit 0 ;;
esac

[ -f "$file" ] || exit 0
command -v yayamlls >/dev/null 2>&1 || exit 0

# `yayamlls validate` prints `path:line:col: severity: message` and exits 1 when
# it finds error-severity diagnostics. Combine stdout+stderr so both the
# diagnostics and any read/parse errors reach Claude.
if out=$(yayamlls validate "$file" 2>&1); then
  exit 0
fi

printf 'yayamlls found issues in %s:\n%s\n' "$file" "$out" >&2
exit 2
