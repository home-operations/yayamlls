---
description: Validate and understand schema-backed YAML (Kubernetes, Flux, and JSON-Schema configs) with the yayamlls language server. Use when editing or reviewing .yaml/.yml files, diagnosing schema validation errors, configuring .yayamlls.yaml, suppressing a diagnostic, or working with Flux HelmRelease / Kustomization rendering.
---

# Working with YAML via yayamlls

`yayamlls` is a YAML language server that resolves a JSON Schema for each
document and reports violations. When this plugin is installed, Claude Code
talks to it over LSP automatically (live diagnostics, hover, completion) and the
bundled hook re-validates a file after every edit. This skill covers the parts
that aren't discoverable from the LSP wire.

## Validate from the command line

The same engine runs as a one-shot CLI — use it to check files without an editor:

```sh
yayamlls validate path/to/file.yaml        # a single file
yayamlls validate ./clusters               # a directory (walks *.yaml/*.yml)
yayamlls validate --root . ./apps          # pin the workspace root explicitly
```

Output is `path:line:col: severity: message`. Exit code is `1` when any
**error**-severity diagnostic was found, `2` on a usage/I-O error, `0` otherwise.
`lint` is an alias for `validate`.

## How a schema is chosen

For each document yayamlls resolves a schema, in priority order:

1. A `# yaml-language-server: $schema=<url-or-path>` modeline at the top.
2. `.yayamlls.yaml` `schemas:` glob mappings (workspace config).
3. Kubernetes/Flux `apiVersion` + `kind` (when `kubernetes` is enabled), and the
   SchemaStore catalog (when `catalog` is enabled).

If no schema resolves, the document gets YAML syntax checks only — no schema
errors. If diagnostics look wrong, first confirm which schema (if any) was
resolved.

## Workspace config: `.yayamlls.yaml`

Lives at the workspace root and is the lowest-precedence settings layer (the
editor/plugin can override it). Common keys:

```yaml
catalog: true               # use the SchemaStore catalog for well-known files
catalogUrl: https://...     # custom catalog
kubernetes:                 # apiVersion+kind detection + Flux rendering
  enabled: true             #   (on by default; set false for generic YAML)
schemas:                    # schema URL/path -> the file globs it applies to
  "https://example.com/my-schema.json":
    - "clusters/**/*.yaml"
fluxSubstitutions: true     # treat ${VAR} Flux postBuild substitutions as valid
customTags: ["!secret"]     # extra YAML tags to accept without error
renderDebounceMs: 200       # debounce for the Flux render pipeline
```

See `.yayamlls.yaml.example` in the repo root for the full set.

## Suppressing a diagnostic

yayamlls honours its own directives in YAML comments. Prefer fixing the YAML;
suppress only when the schema is wrong or a value is intentionally
non-conformant.

```yaml
foo: bar  # yayamlls-disable-line   # suppresses this line
# yayamlls-disable-line
baz: qux                            # suppresses the line below the directive

# yayamlls-disable
# ... suppressed block ...
# yayamlls-enable

# yayamlls-disable-file              # suppresses the whole file (anywhere)
```

The directive must be the first token of the comment.

## Flux rendering (HelmRelease / Kustomization)

With `kubernetes` enabled, opening a Flux `HelmRelease` or `Kustomization`
renders the resource (via the embedded `flate` engine) and surfaces schema
violations from the *rendered* manifests back on the source document, tagged
`[rendered <kind>/<name> @ <jsonptr>]`. So a diagnostic may point at the source
file but describe a problem in the rendered output — read the tag to tell which.

## When editing YAML in this repo

- After changing a `.yaml`/`.yml`, run `yayamlls validate <file>` (the hook does
  this automatically, but run it yourself when working outside an edit hook).
- Treat error-severity diagnostics as blocking; fix them before moving on.
- Don't invent schema URLs — rely on the resolution order above or an explicit
  `schemas:` mapping in `.yayamlls.yaml`.
