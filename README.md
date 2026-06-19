# yayamlls

**Y**et **A**nother **YAML** **L**anguage **S**erver in Go. Schema-driven diagnostics, completion, and
hover; pluggable rendering for Flux `HelmRelease` and `Kustomization`
sources via [home-operations/flate][flate].

Per-document schema resolution, highest priority first:

1. in-file modeline (`# yaml-language-server: $schema=<url>`)
2. workspace `schemas:` glob in `.yayamlls.yaml`
3. JSON Schema Store catalog (filename match)
4. Kubernetes `apiVersion`+`kind` → `kubernetes.schemaUrl` template

Multi-doc files validate each document against its own schema. The
default `kubernetes.schemaUrl` is
`https://k8s-schemas.home-operations.com/{groupSeg}{kindLower}_{versionLower}.json`;
override in `.yayamlls.yaml` to point elsewhere. 404s are silently skipped.

Kubernetes support — apiVersion+kind detection, Flux rendering, and code
lenses — is on by default. Disable it to run as a generic YAML language server:

```yaml
kubernetes:
    enabled: false
```

## vs. redhat/yaml-language-server

|                                                | yayamlls                                       | redhat/yaml-language-server                                 |
| ---------------------------------------------- | ---------------------------------------------- | ----------------------------------------------------------- |
| Runtime                                        | static Go binary                               | Node.js ≥ 12                                                |
| Diagnostics, completion, hover                 | yes                                            | yes                                                         |
| Symbols, folding, links, code actions          | yes                                            | yes                                                         |
| Code lens                                      | rendered output, diff                          | none                                                        |
| Kubernetes auto-detect                         | URL template from apiVersion+kind (toggleable) | `yaml.kubernetesCRDStore` ([datreeio/CRDs-catalog][datree]) |
| Workspace config file                          | `.yayamlls.yaml`                               | editor settings only                                        |
| Flux `HelmRelease` / `Kustomization` rendering | via [flate][flate]                             | no                                                          |
| Pluggable renderers (`kustomize`, `helm`, …)   | config-declared subprocess                     | no                                                          |
| Formatting                                     | no                                             | yes (Prettier)                                              |
| Custom YAML tags (`!Ref`, etc.)                | passthrough (skip validation)                  | yes                                                         |
| Diagnostic suppression comments                | yes (`# yayamlls-disable*`)                    | yes                                                         |
| JSON Schema drafts                             | 04, 06, 07, 2019-09, 2020-12                   | 04, 07, 2019-09, 2020-12                                    |

[datree]: https://github.com/datreeio/CRDs-catalog

## Install

Homebrew:

```sh
brew install home-operations/tap/yayamlls
```

Go:

```sh
go install github.com/home-operations/yayamlls/cmd/yayamlls@latest
```

Prebuilt binaries for linux/darwin/windows (amd64+arm64) are attached to
each [GitHub release](https://github.com/home-operations/yayamlls/releases).

Flux rendering is built in via [flate][flate]; no separate install is needed.

## Command line

`yayamlls validate` (alias `lint`) validates files for CI or ad-hoc checks,
resolving schemas exactly as the editor does. Pass files or directories
(directories are walked for `*.yaml`/`*.yml`):

```sh
yayamlls validate kubernetes/                 # whole tree
yayamlls validate app/helmrelease.yaml ks.yaml
```

Diagnostics print ruff-style, `path:line:col: source message`, and the exit
code is `1` if any error was reported, `2` on a usage/IO error, else `0`.
`--root <dir>` pins the workspace root for `.yayamlls.yaml` (default:
auto-detect).

Add `--render` to also render Flux `HelmRelease`/`Kustomization` documents via
[flate][flate] and validate the rendered manifests — the apply-time check, not
just the source files:

```sh
yayamlls validate --render kubernetes/
```

Rendering shells out to git/helm, so it is opt-in and slower than raw
validation; the render tree is built once per run and reused across documents,
so validating a whole directory in one invocation is far cheaper than
file-by-file. A render that can't resolve a document (e.g. a component base
template with unsubstituted `${vars}`) is reported as a warning and does not
fail the run; only schema violations in rendered output do.

## Editor setup

`yayamlls` speaks LSP 3.16 over stdio. Put the binary on `$PATH` or pass
an absolute path.

Packaged extensions for **VS Code** and **Zed** live in [`editors/`](editors);
they download the matching `yayamlls` release binary automatically.
The snippets below are for editors with built-in LSP support.

### Neovim

Use the built-in `vim.lsp.config`/`vim.lsp.enable` API (0.11+):

```lua
vim.lsp.config("yayamlls", {
  cmd = { "yayamlls" },
  filetypes = { "yaml" },
  root_markers = { ".yayamlls.yaml", ".git" },
})

vim.lsp.enable("yayamlls")
```

With no marker found the server still attaches in single-file mode.

### VSCode

Use the extension in [`editors/vscode`](editors/vscode); it downloads the
`yayamlls` binary on first activation, and exposes `yayamlls.*` settings. To build and run it locally, press <kbd>F5</kbd>
from that directory; to package a `.vsix`, run `vsce package`. See its
[README](editors/vscode/README.md) for settings and publishing.

### Helix

```toml
# ~/.config/helix/languages.toml
[language-server.yayamlls]
command = "yayamlls"

[[language]]
name = "yaml"
language-servers = ["yayamlls"]
```

### Zed

Use the extension in [`editors/zed`](editors/zed); it registers `yayamlls` as a
language server for the YAML language and downloads the binary for you (install
it via **zed: install dev extension**). Since Zed bundles its own
`yaml-language-server`, make `yayamlls` the only one in
`~/.config/zed/settings.json`:

```jsonc
{
    "languages": {
        "YAML": { "language_servers": ["yayamlls", "!yaml-language-server"] },
    },
}
```

Without the extension, Zed's `lsp` key only accepts known language-server
identifiers (`yayamlls` as a top-level key triggers `Property yayamlls is not
allowed`), so the settings-only alternative is to override the bundled
`yaml-language-server` binary:

```jsonc
// ~/.config/zed/settings.json
{
    "lsp": {
        "yaml-language-server": {
            "binary": {
                "ignore_system_version": true,
                "path": "yayamlls",
            },
            "initialization_options": {
                "catalog": true,
            },
        },
    },
}
```

### Gram

[Gram](https://gram-editor.com) is a Zed fork that installs Zed extensions, so use
the same [`editors/zed`](editors/zed) extension with the settings above. Gram
compiles it to WASM locally at install time, so it needs a Rust toolchain that can
target WASM (`rustup target add wasm32-wasip2`) and `clang`; see the
[Gram install docs][gram-install].

Install via the Extension Gallery (`gram::Extensions`) → **Install Local**,
selecting the [`editors/zed`](editors/zed) directory. **Install From URL** clones
the repo and reads `extension.toml` from its root, so it can't reach the
`editors/zed` subdirectory; use Install Local.

[gram-install]: https://gram-editor.com/docs/extensions/installing-extensions/

### Claude Code

`yayamlls` ships as a [Claude Code](https://claude.com/claude-code) plugin in
[`editors/claude`](editors/claude): it registers the language server over LSP
(live diagnostics, hover, completion), adds a hook that runs `yayamlls validate`
after each YAML edit and feeds the results back, and bundles a skill. This repo
is itself a plugin marketplace:

```
/plugin marketplace add home-operations/yayamlls
/plugin install yayamlls@yayamlls
```

The plugin doesn't bundle the binary — install it first (`brew install
home-operations/tap/yayamlls`, `go install`, or a release) so `yayamlls` is on
`$PATH`; the hook also needs `jq`. Full guide: [`editors/claude/README.md`](editors/claude/README.md).

### OpenCode

[OpenCode](https://opencode.ai) registers custom language servers through the
`lsp` key in `opencode.json` (project root or `~/.config/opencode/`). OpenCode
ships a built-in YAML server (Red Hat's `yaml-language-server`, keyed `yaml-ls`),
so disable it alongside the `yayamlls` entry to avoid duplicate diagnostics. Put
the binary on `$PATH`, then:

```jsonc
// opencode.json
{
    "$schema": "https://opencode.ai/config.json",
    "lsp": {
        "yaml-ls": { "disabled": true },
        "yayamlls": {
            "command": ["yayamlls"],
            "extensions": [".yaml", ".yml"]
        }
    }
}
```

Each `lsp` entry also accepts `env` and `initialization` (the server reads
`kubernetes`, `catalog`, `catalogUrl`, `schemas`, `renderers`). Prefer a
workspace `.yayamlls.yaml` for those so the config stays editor-agnostic.

### Flux rendering

Opening a `HelmRelease` or `Kustomization` surfaces schema violations on the
[flate][flate]-rendered manifests as
`[rendered <kind>/<name> @ <jsonptr>]` on the source document. A code lens
on the resource offers **View rendered** and **Diff rendered**; running it
opens the result in the editor via a `window/showDocument` request, so no
client-specific glue is needed.

Clients that open local-file `showDocument` requests display it directly —
Neovim ≥ 0.11 and VS Code do. Zed currently no-ops local-file `showDocument`
([zed#53123][zed-showdoc]), so the lens runs but nothing opens; it will work
unchanged once Zed supports it. Other features (diagnostics, completion, hover,
code actions) are unaffected everywhere.

[zed-showdoc]: https://github.com/zed-industries/zed/discussions/53123

### Debugging

```sh
yayamlls --log-file /tmp/yayamlls.log -v 2
```

`-v 0` is silent (default), `1` is info, `2+` is debug.

## Configuration

`.yayamlls.yaml` in the workspace root:

```yaml
schemas:
    "https://json.schemastore.org/github-workflow.json":
        - ".github/workflows/*.yml"
    "./schemas/local.json":
        - "k8s/**/*.yaml"

catalog: true
catalogUrl: ""

# Optional. Kubernetes apiVersion+kind auto-detect (on by default). Set
# enabled: false to run as a generic YAML language server. schemaUrl overrides
# the lookup template; placeholders: {group}, {groupSeg}, {groupFirst},
# {groupFirstSeg}, {kind}, {kindLower}, {version}, {versionLower}.
# kubernetes:
#   enabled: false
#   schemaUrl: "https://schemas.example.com/{groupSeg}{kindLower}_{versionLower}.json"

# Optional. Defaults shown.
# renderers:
#   flate:
#     enabled: true
#     # Narrow the Flux entry flate builds from (defaults to the workspace
#     # root), so a HelmRelease resolves a source defined elsewhere. Output is
#     # scoped to the edited resource by metadata.name. Relative to workspace root.
#     path: kubernetes
#   # Declare your own renderer for any kind by shelling out to a command.
#   # No recompile needed — flate is just the built-in version of this.
#   kustomize:
#     match: { kind: Kustomization, group: kustomize.toolkit.fluxcd.io }
#     command: ["kustomize", "build", "{dir}"]

# Optional. Debounce (ms) before a document change triggers a renderer.
# Default: 750.
# renderDebounceMs: 750

# Optional. Max time (ms) a single render may run before its deadline trips.
# Raise it if a slow source fetch (git/OCI) trips "context deadline exceeded".
# Default: 30000.
# renderTimeoutMs: 30000

# Optional. YAML tags resolved by an external tool (Flux, CloudFormation,
# Vault, …). Nodes carrying one skip schema validation, since the value
# present in the file is a placeholder, not the resolved value.
# customTags:
#   - "!Ref"
#   - "!vault"
```

By default `flate` builds from the workspace root, following Flux's `spec.path`
references to resolve sources (such as an `OCIRepository`) defined elsewhere and
scoping the build to the edited resource's `metadata.name`. Set
`renderers.flate.path` (typically your cluster root) to point at a narrower Flux
entry; relative paths anchor at the workspace root.

### Custom renderers

A `renderers:` entry with `match` and `command` declares a subprocess
renderer, so any tool that prints Kubernetes YAML can drive the rendered-output
code lens and diagnostics — no recompile. `match` selects documents by `kind`
(and optional `group`, matched on a group boundary). `command` is the argv to
run; its stdout is parsed as multi-document YAML. Placeholders: `{dir}` (the
document's directory), `{file}` (its path), `{name}` (`metadata.name`). The
command runs with its working directory set to the document's directory.
A config-declared renderer takes precedence over a built-in one matching the
same kind, and a missing command binary is treated as "renderer unavailable"
(silent).

See [`.yayamlls.yaml.example`](.yayamlls.yaml.example) for a copyable starter.

Same shape works via `initializationOptions` or
`workspace/didChangeConfiguration`. Precedence (low → high):
`.yayamlls.yaml` → `initializationOptions` → `didChangeConfiguration`.

## Suppressing diagnostics

Comments mute diagnostics so the language server stops reporting them:

```yaml
age: not-a-number  # yayamlls-disable-line

# yayamlls-disable-line
age: not-a-number

# yayamlls-disable
foo: bad
bar: also-bad
# yayamlls-enable
```

- `# yayamlls-disable-line`: trailing a value, suppresses that line; on its
  own line, suppresses the line below.
- `# yayamlls-disable` / `# yayamlls-enable`: suppress every line in between.
- `# yayamlls-disable-file`: suppress the whole file (place it anywhere).

## Capabilities

`textDocument/`: diagnostics, completion (trigger characters `:`, ` `, `-`;
snippet expansion when the client supports it), hover, foldingRange,
documentLink, documentSymbol, codeAction (enum + suppress quick-fix), codeLens.

`workspace/`: didChangeConfiguration, didChangeWorkspaceFolders,
didChangeWatchedFiles (config hot-reload + render cache invalidation),
executeCommand.

## Commands

- `yayamlls.showRendered <uri>`: rendered output for a Flux source.
- `yayamlls.showRenderedDiff <uri>`: unified diff between the open-time
  render and the current render.

## CLI flags

```
yayamlls --version              print version and exit
yayamlls --log-file PATH        append logs to PATH instead of stderr
yayamlls -v N                   log verbosity (0=silent, 1=info, 2+=debug)
```

## Validate (one-shot, for CI)

`yayamlls validate` (alias `lint`) checks files without an editor, resolving
schemas the same way the server does (modeline, `.yayamlls.yaml` globs,
catalog, Kubernetes auto-detect) and honouring `# yayamlls-disable*`
comments. Directory arguments are walked for `*.yaml`/`*.yml`.

```sh
yayamlls validate deploy.yaml            # one file
yayamlls validate k8s/                   # walk a directory
yayamlls validate --root . manifests/    # pin the workspace root for .yayamlls.yaml
```

Diagnostics print as `path:line:col: severity: message`. The exit code is
`1` when any error-severity diagnostic is reported, `2` on a usage or I/O
error, `0` otherwise. The workspace root is auto-detected (nearest
`.yayamlls.yaml` or `.git`) unless `--root` is given.

## Development

```sh
mise install   # toolchain
mise run test
mise run lint
mise run build
```

[flate]: https://github.com/home-operations/flate
