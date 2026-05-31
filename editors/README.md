# Editor integrations

`yayamlls` speaks LSP 3.16 over stdio, so every editor here is a thin client that
launches the binary and forwards `yayamlls.*` settings as `initializationOptions`
(`schemas`, `catalog`, `catalogUrl`, `kubernetes.schemaUrl`, `renderers`).

Both extensions **download the matching release binary automatically** from
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

For editors with built-in LSP support (Neovim, Helix) you don't need an
extension; see the "Editor setup" section of the top-level [README](../README.md).
