# Changelog

## [0.0.12](https://github.com/home-operations/yayamlls/compare/0.0.11...0.0.12) (2026-06-03)


### Features

* add gram installation guide ([09337ba](https://github.com/home-operations/yayamlls/commit/09337ba6fd91d05706c6263f9c05da20630f8cff))
* add kubernetes.enabled toggle for generic YAML mode ([11906e4](https://github.com/home-operations/yayamlls/commit/11906e43a3f44b6a4bbfd2e9ce4cc4e54aa299e5))
* **mise:** update tool oxfmt (0.52.0 → 0.53.0) ([e1728df](https://github.com/home-operations/yayamlls/commit/e1728df21ebf0ee04a7102309b72a8a0fa0584d8))
* **mise:** update tool oxfmt (0.52.0 → 0.53.0) ([ab27e22](https://github.com/home-operations/yayamlls/commit/ab27e22eb0a267af7408178805bcbf61dfc570d7))


### Bug Fixes

* **mise:** update tool go (1.26.3 → 1.26.4) ([60c667c](https://github.com/home-operations/yayamlls/commit/60c667c7c4d10bf3143f39de1cfa493269d8dad1))

## [0.0.11](https://github.com/home-operations/yayamlls/compare/0.0.10...0.0.11) (2026-06-01)


### Bug Fixes

* **zed:** set extension version to 0.0.10 ([edc395d](https://github.com/home-operations/yayamlls/commit/edc395db54346b256bbbd0250a39256704f98be8))


### Miscellaneous Chores

* **vscode:** add MIT license for VSCode extension ([58b4c5a](https://github.com/home-operations/yayamlls/commit/58b4c5a0d5a882ac2d6e5cd79227f74516eb59dd))
* **zed:** add workflow to keep Cargo.lock in sync and update for 0.0.10 ([603d0c0](https://github.com/home-operations/yayamlls/commit/603d0c0e3a0ba2d7f21594d5683d5cae58b337bc))

## [0.0.10](https://github.com/home-operations/yayamlls/compare/0.0.9...0.0.10) (2026-06-01)


### ⚠ BREAKING CHANGES

* **deps:** Update dependency typescript (5.9.3 → 6.0.3) ([#21](https://github.com/home-operations/yayamlls/issues/21))
* **deps:** Update module github.com/santhosh-tekuri/jsonschema/v5 (v5.3.1 → v6.0.2) ([#22](https://github.com/home-operations/yayamlls/issues/22))
* **github-release:** Update release node (20.20.2 → 24.16.0) ([#23](https://github.com/home-operations/yayamlls/issues/23))

### Features

* **deps:** Update dependency typescript (5.9.3 → 6.0.3) ([#21](https://github.com/home-operations/yayamlls/issues/21)) ([c0756cc](https://github.com/home-operations/yayamlls/commit/c0756ccd0c2bd64561689fbd43e8dceee0cb69eb))
* **deps:** update module github.com/goccy/go-yaml (v1.11.3 → v1.19.2) ([#20](https://github.com/home-operations/yayamlls/issues/20)) ([81d9fe3](https://github.com/home-operations/yayamlls/commit/81d9fe3ad430e5ae902519066ce99fa7161e13f1))
* **deps:** Update module github.com/santhosh-tekuri/jsonschema/v5 (v5.3.1 → v6.0.2) ([#22](https://github.com/home-operations/yayamlls/issues/22)) ([0c730df](https://github.com/home-operations/yayamlls/commit/0c730df25059686192a282d1bb63cfdd3307dbc6))
* flux substitution support ([e517f18](https://github.com/home-operations/yayamlls/commit/e517f187c02efdfd4b2f3e891a8092c6bd1299ab))
* **github-release:** Update release node (20.20.2 → 24.16.0) ([#23](https://github.com/home-operations/yayamlls/issues/23)) ([8ecf62c](https://github.com/home-operations/yayamlls/commit/8ecf62c8928c004fc767eaf24dcd46a91cc643b2))


### Bug Fixes

* **mise:** update tool lefthook (2.1.8 → 2.1.9) ([015a330](https://github.com/home-operations/yayamlls/commit/015a3305ce21206f0f4f0946d71297e8fc53c84d))

## [0.0.9](https://github.com/home-operations/yayamlls/compare/0.0.8...0.0.9) (2026-05-31)


### Features

* **lsp:** add quick-fix code action to suppress a diagnostic ([e7fe753](https://github.com/home-operations/yayamlls/commit/e7fe75360fdd993c42a36024aa86c4a8bcb4fe7e))
* **lsp:** advertise supported codeAction kinds ([62b2ef1](https://github.com/home-operations/yayamlls/commit/62b2ef13fbd21bf66d46f6bc145db7b4c0fa450b))
* **lsp:** open rendered output via window/showDocument ([e9fdec4](https://github.com/home-operations/yayamlls/commit/e9fdec41cef67f7932df817a10c2788f6af4b478))
* **lsp:** open rendered output via window/showDocument ([5d8f9e5](https://github.com/home-operations/yayamlls/commit/5d8f9e56648bf5cde119a3186031a53ea45a5d74))


### Miscellaneous Chores

* **docs:** update neovim lsp instructions for 0.11+ ([d966720](https://github.com/home-operations/yayamlls/commit/d966720b033a6c271030b3437002d614f1720b6d))
* **docs:** update neovim lsp instructions for 0.11+ ([a4657dd](https://github.com/home-operations/yayamlls/commit/a4657ddeb807a535e02b113624a6612ad1d17726))
* implement oxfmt ([4945553](https://github.com/home-operations/yayamlls/commit/494555369a94c495b8f46d856aa760caa2f10830))
* remove default draft-pull-request from release-please config ([66db1b0](https://github.com/home-operations/yayamlls/commit/66db1b062b09860afa98598f1e393e410d372846))

## [0.0.8](https://github.com/home-operations/yayamlls/compare/0.0.7...0.0.8) (2026-05-31)


### Bug Fixes

* **lsp:** don't block the message loop on schema fetches ([d35ba98](https://github.com/home-operations/yayamlls/commit/d35ba98d2fc88b54b0afdef3a345a9b07ea19b95))


### Documentation

* Explain new name in first line of readme ([b3c93ec](https://github.com/home-operations/yayamlls/commit/b3c93ecadb64a7a07c71b6a72e4a7542e23d793f))
* Explain new name in first line of readme ([f82cb5e](https://github.com/home-operations/yayamlls/commit/f82cb5ee20899cd7b185334238728166d7442024))

## [0.0.7](https://github.com/home-operations/yayamlls/compare/0.0.6...0.0.7) (2026-05-30)


### Features

* Ability to set flux repo root ([e7c7657](https://github.com/home-operations/yayamlls/commit/e7c7657d886fb5e3789ba8ecc72f196710d453e2))

## [0.0.6](https://github.com/home-operations/yamlls/compare/0.0.5...0.0.6) (2026-05-29)


### Features

* add validate command ([866f1ea](https://github.com/home-operations/yamlls/commit/866f1eab4b8ff7ee9846e6c15a3210de4001a25e))


### Performance Improvements

* improve multidoc speed ([ea82273](https://github.com/home-operations/yamlls/commit/ea82273abaa16ff8b63c0d962f63da57f4df594b))
* parallelize validation ([ef6d054](https://github.com/home-operations/yamlls/commit/ef6d0544dbe20d1c1da064fced85bfe13e94151c))


### Miscellaneous Chores

* fix lint ([a5f8d07](https://github.com/home-operations/yamlls/commit/a5f8d07587d7eb665161e7803e520433cf705875))

## [0.0.5](https://github.com/home-operations/yamlls/compare/0.0.4...0.0.5) (2026-05-29)


### Features

* handle cancelRequest ([6f4ccc7](https://github.com/home-operations/yamlls/commit/6f4ccc7418289b6eafd55eafe1d44a37420b0283))


### Bug Fixes

* **config:** carry kubernetes settings through Merge ([0709a67](https://github.com/home-operations/yamlls/commit/0709a6782c2918b23bd713abb653c0b4a442a4d6))
* **diagnostics:** anchor YAML parse errors at their reported position ([98148d0](https://github.com/home-operations/yamlls/commit/98148d09cce3570f84a428ab763d442bee8875da))
* **document:** count UTF-16 code units when applying incremental edits ([91c5074](https://github.com/home-operations/yamlls/commit/91c5074dbc27a94854d21ca30edddfa054e45068))
* **document:** replace documents instead of mutating to avoid a render-goroutine data race ([9ff0002](https://github.com/home-operations/yamlls/commit/9ff00027260db33daf114c492c0c7e92ac56a2e0))
* **flate:** guard binary resolution with the mutex to remove a data race ([1c90dad](https://github.com/home-operations/yamlls/commit/1c90dadc2de3be80402d07922a85a1a84872398c))
* **lsp:** emit diagnostic and symbol ranges in UTF-16 columns ([33ebed3](https://github.com/home-operations/yamlls/commit/33ebed37d47eb57b2563e26e076403460bd421b8))
* **lsp:** publish empty diagnostics array so clients clear stale entries ([96fd32f](https://github.com/home-operations/yamlls/commit/96fd32f08134c1661b9209cf2d6b508c87788036))
* **lsp:** track workspace and override settings layers so a folder change preserves client config ([702fde9](https://github.com/home-operations/yamlls/commit/702fde973b5f5d48f7b8e8b9f0f1e76d5ea1dace))
* **render:** bound the render cache per-URI and evict it on document close ([7520dfa](https://github.com/home-operations/yamlls/commit/7520dfaf5d5867fb5c9b9e0ac2dbfecd67886a72))
* **render:** cancel the render context when the debounced render finishes ([87aa49a](https://github.com/home-operations/yamlls/commit/87aa49a561cb58e0f7705e690aaf737f2df156ad))
* **render:** drop superseded renders so stale results can't overwrite fresh diagnostics ([72591c7](https://github.com/home-operations/yamlls/commit/72591c7235d1ceaf19c39c2e1915b8672af81370))
* **render:** require a group boundary in MatchesKind to avoid over-matching ([953ec5b](https://github.com/home-operations/yamlls/commit/953ec5b349ddbed861fa1cc5e9be9897cbf5ccbe))
* **render:** stay silent when the flate binary is not installed ([cce2192](https://github.com/home-operations/yamlls/commit/cce21929a73303fd8e205106cb0f610ea0e2b05d))
* **schema:** bound schema fetches and release the store lock during compile ([49b55a0](https://github.com/home-operations/yamlls/commit/49b55a0982958989da52700a3d577f4025757adf))
* **schema:** load the schema catalog in the background instead of on the request path ([555ae2c](https://github.com/home-operations/yamlls/commit/555ae2c6619a7399351bc29ce5672d94dbd340d4))
* **schema:** lowercase {kind} and {version} placeholders per the documented contract ([ad2bf22](https://github.com/home-operations/yamlls/commit/ad2bf22c5fdd7ed6dbe096958ac524fdab9c8600))
* **schema:** resolve patternProperties and recursive/dynamic refs during traversal ([e4ea274](https://github.com/home-operations/yamlls/commit/e4ea27432effa4764f06138fd288d27a5c0350c2))
* **uri:** convert Windows file URIs and drive paths correctly ([a4b3be4](https://github.com/home-operations/yamlls/commit/a4b3be446643909969b17aba1e1971f0a0139ca6))
* **yamlast:** build valid JSON pointers from goccy paths with quoted or slashed keys ([af2bdbc](https://github.com/home-operations/yamlls/commit/af2bdbcc13c1beadf496bb257d7000967248ff14))
* **yamlast:** map cursor positions in UTF-16 and clamp past-end-of-line offsets ([624d44a](https://github.com/home-operations/yamlls/commit/624d44afc5c4966caa84e4e3d2e9b0775ce5ca97))
* **yamlast:** resolve JSON pointers by AST traversal so numeric and dotted keys map correctly ([0fb1df9](https://github.com/home-operations/yamlls/commit/0fb1df95ca7a90a0caed491434b1d61cf478705b))


### Styles

* **lsp:** alias the uri import to avoid shadowing local variables ([df48e10](https://github.com/home-operations/yamlls/commit/df48e10860cea1fdff5f481f451a3db07a1577a4))

## [0.0.4](https://github.com/home-operations/yamlls/compare/0.0.3...0.0.4) (2026-05-24)


### Miscellaneous Chores

* yoink flate release setup ([670f23c](https://github.com/home-operations/yamlls/commit/670f23ca6cde2298e1f6f4f922a703befeef224f))

## [0.0.3](https://github.com/home-operations/yamlls/compare/0.0.2...0.0.3) (2026-05-23)


### Bug Fixes

* analyze correctly when files has multiple schema ([6f458a2](https://github.com/home-operations/yamlls/commit/6f458a2b36140481ab60ba2768f8c03e5454ba45))


### Miscellaneous Chores

* refactor Homebrew cask configuration in goreleaser ([dea67bf](https://github.com/home-operations/yamlls/commit/dea67bfe004c6eced22dd0ccf2550e5b682f8647))

## [0.0.2](https://github.com/home-operations/yamlls/compare/0.0.1...0.0.2) (2026-05-22)


### Features

* homebrew tap via goreleaser homebrew_casks ([84b4c46](https://github.com/home-operations/yamlls/commit/84b4c46b7b6b4844e35b6763697e9a34bea2dbb7))
* **lsp:** code actions, code lenses, render diff, .yamlls.yaml self-validate ([77e1aef](https://github.com/home-operations/yamlls/commit/77e1aef26f8547fb090a94381ad85c6d3ad8da4a))
* **lsp:** foldingRange, documentLink, documentSymbol, workspaceFolders ([f7223f6](https://github.com/home-operations/yamlls/commit/f7223f68991f820cdcf056d424eedd2b388c1022))


### Miscellaneous Chores

* strip explanatory cruft from comments and docs ([83105a7](https://github.com/home-operations/yamlls/commit/83105a73190b7db16a4758f0c73a20c2e3a13e22))
* strip explanatory cruft from the new lsp features ([a0e4d28](https://github.com/home-operations/yamlls/commit/a0e4d28627351439772cb8b825bf460a5533f84b))

## 0.0.1 (2026-05-22)


### Features

* initial yamlls language server ([e8438c3](https://github.com/home-operations/yamlls/commit/e8438c38ca9f013ead00707053f8c140578f35b5))
* **schema:** configurable kubernetes.schemaUrl template ([762086c](https://github.com/home-operations/yamlls/commit/762086c915171fc9b48d1ffb0b2a0c5049ce13d9))
* **schema:** default kubernetes.schemaUrl to home-operations mirror ([4e738db](https://github.com/home-operations/yamlls/commit/4e738db6b2dbcc6db483dea87edd0aa4dc83a6d5))


### Bug Fixes

* resolve Kubernetes schema per-document for multi-doc files ([1c78538](https://github.com/home-operations/yamlls/commit/1c785389fe4b50c2e1db418cca60a1a62514ef15))
* skip leading `---` separator when scanning for modeline ([daa02b4](https://github.com/home-operations/yamlls/commit/daa02b4aa0259f9efecf5955f8e15337dedf7727))


### Documentation

* surface CLI flags, comment out optional config, fix VSCode example ([0b1b13c](https://github.com/home-operations/yamlls/commit/0b1b13c48a509358e85f0c27e1aa331ba8c54e96))


### Miscellaneous Chores

* release 0.0.1 ([5c3293b](https://github.com/home-operations/yamlls/commit/5c3293b3b97e93af4b5f4717eefc6a3c4a06a63d))

## Changelog
