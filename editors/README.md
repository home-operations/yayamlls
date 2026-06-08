# Editor integrations

`yayamlls` speaks LSP 3.16 over stdio, so every editor here is a thin client that
launches the binary and forwards `yayamlls.*` settings as `initializationOptions`
(`schemas`, `catalog`, `catalogUrl`, `kubernetes.schemaUrl`, `renderers`).

Every extension **downloads the matching release binary automatically** from
[GitHub releases](https://github.com/home-operations/yayamlls/releases), selecting
the asset for the host OS/arch:

```
yayamlls_{version}_{os}_{arch}.tar.gz   # linux, darwin
yayamlls_{version}_{os}_{arch}.zip      # windows
```

| Editor  | Language                             | Subdir                | Binary source                                                     |
| ------- | ------------------------------------ | --------------------- | ----------------------------------------------------------------- |
| VS Code | TypeScript (`vscode-languageclient`) | [`vscode/`](./vscode) | downloaded to global storage; override with `yayamlls.path`       |
| Zed     | Rust → WASM (`zed_extension_api`)    | [`zed/`](./zed)       | downloaded to the extension work dir; override with `binary.path` |

**Claude Code** consumes `yayamlls` as a plugin (LSP + validate-on-edit hook +
skill) rather than an editor extension; the binary must already be on `$PATH`.
See [`claude/`](./claude) and the top-level [README](../README.md#claude-code).
**OpenCode** uses its own `opencode.json` `lsp` config — see the top-level
[README](../README.md#opencode).

**Gram** is a Zed fork that installs Zed extensions, so point it at [`zed/`](./zed)
via **Install Local**; it compiles to WASM locally and needs a WASM-capable Rust
toolchain (`wasm32-wasip2`, vs. Zed's `wasm32-wasip1`). See the top-level
[README](../README.md#gram).

For editors with built-in LSP support (Neovim, Helix) you don't need an
extension; see the "Editor setup" section of the top-level [README](../README.md).
