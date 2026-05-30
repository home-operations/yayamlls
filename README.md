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

## vs. redhat/yaml-language-server

|                                                | yayamlls                            | redhat/yaml-language-server                                 |
| ---------------------------------------------- | --------------------------------- | ----------------------------------------------------------- |
| Runtime                                        | static Go binary                  | Node.js ≥ 12                                                |
| Diagnostics, completion, hover                 | yes                               | yes                                                         |
| Symbols, folding, links, code actions          | yes                               | yes                                                         |
| Code lens                                      | rendered output, diff             | none                                                        |
| Kubernetes auto-detect                         | URL template from apiVersion+kind | `yaml.kubernetesCRDStore` ([datreeio/CRDs-catalog][datree]) |
| Workspace config file                          | `.yayamlls.yaml`                    | editor settings only                                        |
| Flux `HelmRelease` / `Kustomization` rendering | via [flate][flate]                | no                                                          |
| Formatting                                     | no                                | yes (Prettier)                                              |
| Custom YAML tags (`!Ref`, etc.)                | no                                | yes                                                         |
| Diagnostic suppression comments                | yes (`# yayamlls-disable*`)         | yes                                                         |
| JSON Schema drafts                             | 04, 06, 07, 2019-09, 2020-12      | 04, 07, 2019-09, 2020-12                                    |

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

For Flux rendering:

```sh
go install github.com/home-operations/flate/cmd/flate@latest
```

## Editor setup

`yayamlls` speaks LSP 3.16 over stdio. Put the binary on `$PATH` or pass
an absolute path.

Packaged extensions for **VS Code** and **Zed** live in [`editors/`](editors);
they download the matching `yayamlls` (and `flate`) release binary automatically.
The snippets below are for editors with built-in LSP support.

### Neovim (nvim-lspconfig)

```lua
local lspconfig = require("lspconfig")
local configs = require("lspconfig.configs")

if not configs.yayamlls then
  configs.yayamlls = {
    default_config = {
      cmd = { "yayamlls" },
      filetypes = { "yaml" },
      root_dir = lspconfig.util.find_git_ancestor,
      single_file_support = true,
    },
  }
end

lspconfig.yayamlls.setup({})
```

### VSCode

Use the extension in [`editors/vscode`](editors/vscode); it downloads the
`yayamlls` binary (and `flate` for Flux rendering) on first activation, and
exposes `yayamlls.*` settings. To build and run it locally, press <kbd>F5</kbd>
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

### Flux rendering

With [flate][flate] installed, opening a `HelmRelease` or
`Kustomization` surfaces schema violations on rendered manifests as
`[rendered <kind>/<name> @ <jsonptr>]` on the source document. The
`yayamlls.showRendered` command returns the rendered YAML; in Neovim:

```lua
vim.lsp.buf.execute_command({
  command = "yayamlls.showRendered",
  arguments = { vim.uri_from_bufnr(0) },
})
```

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

# Optional. Override the URL template for Kubernetes auto-detect.
# Placeholders: {group}, {groupSeg}, {groupFirst}, {groupFirstSeg},
# {kind}, {kindLower}, {version}, {versionLower}.
# kubernetes:
#   schemaUrl: "https://schemas.example.com/{groupSeg}{kindLower}_{versionLower}.json"

# Optional. Defaults shown.
# renderers:
#   flate:
#     enabled: true
#     binary: flate
#     # Scan from this path instead of the edited file's directory, so a
#     # HelmRelease can resolve a source defined elsewhere. Output is scoped
#     # to the edited resource by metadata.name. Relative to workspace root.
#     path: kubernetes
```

By default `flate` renders the edited file's own directory, which fails when a
`HelmRelease` references a source (such as an `OCIRepository`) defined
elsewhere. Set `renderers.flate.path` (typically your cluster root) and
`flate` follows Flux's `spec.path` references to resolve those dependencies.

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

`textDocument/`: diagnostics, completion, hover, foldingRange,
documentLink, documentSymbol, codeAction (enum quick-fix), codeLens.

`workspace/`: didChangeConfiguration, didChangeWorkspaceFolders, executeCommand.

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
