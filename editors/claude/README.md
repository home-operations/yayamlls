# yayamlls — Claude Code plugin

Connects the `yayamlls` language server to [Claude Code](https://claude.com/claude-code).
Bundles three components:

| Component | What it does |
| --------- | ------------ |
| **LSP server** (`.lsp.json`) | Registers `yayamlls` for live diagnostics, hover, and completion on `.yaml`/`.yml` files. |
| **Hook** (`hooks/hooks.json`) | Runs `yayamlls validate` after each YAML edit and feeds error-severity diagnostics back to Claude. |
| **Skill** (`skills/yaml/`) | Documents schema resolution order, `.yayamlls.yaml`, suppressions, and Flux rendering. Invokable as `/yayamlls:yaml`. |

## Prerequisite: install the binary

A Claude Code LSP/hook plugin configures *how* Claude Code reaches a language
server — it does **not** ship the binary. Put `yayamlls` on your `$PATH` first:

```sh
brew install home-operations/tap/yayamlls
# or: go install github.com/home-operations/yayamlls/cmd/yayamlls@latest
# or: download a release from https://github.com/home-operations/yayamlls/releases
```

Verify: `yayamlls --version`. The hook also needs `jq` on `$PATH`.

> If you see `Executable not found in $PATH` in the `/plugin` **Errors** tab, the
> binary isn't installed (or not on the PATH Claude Code sees).

## Install the plugin

This repository is itself a plugin marketplace, so:

```
/plugin marketplace add home-operations/yayamlls
/plugin install yayamlls@yayamlls
```

(The `@yayamlls` suffix is the marketplace name.) Restart Claude Code when
prompted. To develop locally against a clone, point the marketplace at the repo
path instead:

```
/plugin marketplace add /path/to/yayamlls
```

Validate the plugin before publishing changes:

```sh
claude plugin validate ./editors/claude --strict
```

## Configuration

The LSP entry pins no server options, so a workspace `.yayamlls.yaml` stays
authoritative. Kubernetes/Flux schema detection is on by default; to also enable
the SchemaStore catalog, add a `.yayamlls.yaml` at your project root:

```yaml
catalog: true
```

To run as a generic YAML server instead, disable Kubernetes (note the nested
shape — `kubernetes` is a block, not a bool):

```yaml
kubernetes:
  enabled: false
```

See the repo's [`.yayamlls.yaml.example`](../../.yayamlls.yaml.example) for every
key, and [`skills/yaml/SKILL.md`](skills/yaml/SKILL.md) for how schemas are
resolved.

## Customizing the LSP entry

Claude Code only wires LSP servers through plugins (there is no standalone
`.lsp.json` outside a plugin), so to tweak the connection, edit this plugin's
[`.lsp.json`](.lsp.json). Beyond the required `command` and `extensionToLanguage`,
Claude Code honours these optional fields: `args`, `transport` (`stdio` default),
`env`, `initializationOptions` (e.g. `{"catalog": true}`), `settings`
(delivered via `workspace/didChangeConfiguration`), `workspaceFolder`,
`startupTimeout`, and `maxRestarts`.

Prefer a workspace `.yayamlls.yaml` over `initializationOptions` for server
options: init options are a higher-precedence layer, so pinning them here would
stop a project's `.yayamlls.yaml` from overriding them.

After editing a loaded plugin, run `/reload-plugins` to pick up the change.
