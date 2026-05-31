# yayamlls for Zed

Registers [`yayamlls`](https://github.com/home-operations/yayamlls) as a language
server for Zed's built-in YAML language. The extension downloads the release
binary matching your OS/arch on first use.

## Develop / test locally

Zed → command palette → **zed: install dev extension**, then select this
directory. Zed compiles the Rust crate to WASM (`wasm32-wasip1`) for you; you
only need the toolchain installed (`rustup target add wasm32-wasip1`).

## Make yayamlls the only YAML server

Zed bundles `yaml-language-server`. To run `yayamlls` instead, in
`~/.config/zed/settings.json`:

```jsonc
{
    "languages": {
        "YAML": {
            "language_servers": ["yayamlls", "!yaml-language-server"],
        },
    },
}
```

## Configuration

Pass server options under the `yayamlls` LSP key; they are forwarded verbatim as
`initializationOptions`:

```jsonc
{
    "lsp": {
        "yayamlls": {
            "binary": {
                "path": "/usr/local/bin/yayamlls", // optional: skip the download
            },
            "initialization_options": {
                "catalog": true,
                "schemas": {
                    "https://json.schemastore.org/github-workflow.json": [
                        ".github/workflows/*.yml",
                    ],
                },
            },
        },
    },
}
```

## Publish

Open a PR adding this extension to
[`zed-industries/extensions`](https://github.com/zed-industries/extensions)
(commit `Cargo.lock`).
