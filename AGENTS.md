# AGENTS.md: yayamlls

Guidance for AI coding agents (and humans) writing Go in this repository.
These are the home-operations fleet's shared Go conventions, kept in sync
word-for-word across every Go service/CLI in the org; propagate any change
here to the others.

## Working in this repo: AI usage, commits, and safety

This repo doesn't carry its own `CONTRIBUTING.md`; GitHub serves the org-wide
one from [`home-operations/.github`](https://github.com/home-operations/.github/blob/main/CONTRIBUTING.md),
which includes an AI Usage Policy that applies to any AI coding agent here:
assistive use only, a human must author the majority of any change, AI use
must be disclosed, a human reviews every line before submission, and the
contributor must be able to explain any line a reviewer asks about. AI must
never write the PR description, an issue, or a reply to a human on the
contributor's behalf. Read the policy itself rather than trusting this
summary; it can change without this file being updated to match.

- PR titles follow [Conventional Commits](https://www.conventionalcommits.org/en/v1.0.0/):
  `<type>[(scope)][!]: <description>` (e.g. `fix(config): reject a negative
timeout`), which is what drives release-please's version bumps. Individual
  commit messages don't have to follow the format, though matching it is
  fine. Sign off commits: `git commit -s`.
- Never `git commit`, `git push`, or open a PR unless asked to. Ask before
  any destructive or hard-to-reverse action (force-push, `git reset --hard`,
  deleting a branch, rewriting history) instead of defaulting to it.
- Never touch secrets or gitignored files (`*.key`, `*.crt`, `.env`,
  anything `.gitignore` matches). This fleet passes signing keys and
  webhook secrets by path or env var specifically so they're never
  committed; don't be the exception.
- Don't state a library's API, flags, or defaults from memory: verify
  against `pkg.go.dev`, the vendored source in the module cache, or this
  project's own code. This fleet's dependencies (`caarlos0/env`,
  `spf13/pflag`, `k8s.io/client-go`, and friends) change behavior between
  major versions in ways that are easy to get subtly wrong from
  recollection alone.
- After a change, run `mise run test` and `mise run lint` before calling it
  done. Don't claim untested code works.

## Baseline

- **Idiomatic.** Follow [Effective Go](https://go.dev/doc/effective_go) and
  the [Code Review Comments](https://go.dev/wiki/CodeReviewComments) wiki.
  `gofmt -s` runs on every staged `.go` file via lefthook and again in CI:
  never hand-format, and don't fight it with inline exceptions. Comments
  explain non-obvious constraints only (a hidden invariant, why a workaround
  exists, what would surprise a reader); don't narrate what good naming
  already says, and don't reference the current change or past behavior in
  a comment: that belongs in the PR description and rots as the code moves
  on.
- **Go 1.26**, matching `.mise/config.toml`'s `tools.go` and the `go`
  directive in `go.mod`; bump both together, never one without the other.
  When a newer construct is genuinely more idiomatic, use it: Go 1.26 added
  `errors.AsType[T](err)`, a generic type-safe replacement for the
  `var t *T; errors.As(err, &t)` two-step; prefer it in new code. `go fix`
  (rebuilt in 1.26 as a modernizer runner on `go vet`'s analysis) surfaces
  these mechanical migrations; run it after a toolchain bump.
- **Idempotent.** Reconcilers, code generators (`mise run generate`), and CLI
  subcommands must be safe to re-run: identical input yields identical
  output/state, with no accumulating side effects on a second invocation.
  `ocharted` is the clearest fleet example: every cache entry is
  re-derivable from upstream, so a restart or an extra replica never
  affects correctness, only latency. For this repo: re-linting the same
  document (e.g. on every keystroke, via `tliron/glsp`) must produce the
  same diagnostics without leaking state between requests.
- **DRY and minimal, without premature abstraction.** Three similar call
  sites are fine as-is; don't introduce an interface, options struct, or
  generic helper until a real third caller needs the variance it buys.
  Touch only what the task requires: don't refactor or "improve" adjacent
  code, and match the existing style even where you'd do it differently.
  Remove imports, variables, and functions your own change orphaned; leave
  pre-existing dead code alone and mention it instead of deleting it
  unprompted.
- **Unit tested**, table-driven via `t.Run` subtests. Stdlib `testing` is the
  default and is sufficient for most of this fleet's tests (config parsing,
  parser/linter logic, pure functions); reach for `testify`
  (`assert`/`require`) when a table's per-case assertions get repetitive.
  `go test -race` is the floor (`mise run test`); don't submit
  goroutine-touching code you haven't run under `-race`.
- **`log/slog`**, JSON handler to stdout by default (a text-format escape
  hatch via config is fine for local runs), never a third-party logging
  library, including for the LSP transport itself; adapt `slog` to
  `tliron/commonlog` only at the `glsp` boundary, don't let `commonlog`
  become the primary logging interface elsewhere in the codebase. Call
  `slog.SetDefault` once in `main`, then use package-level
  `slog.Info`/`slog.Error`/etc.
- **`github.com/caarlos0/env/v11`** for configuration, if/when this repo
  grows beyond LSP-client-supplied settings: one `Config` struct in
  `internal/config`, populated by `env.Parse`/`env.ParseAs`, behind a
  `Load()` that also derives any computed fields and validates: fail fast
  on invalid config at startup instead of letting a bad value surface later
  as a runtime error.
- **`github.com/spf13/pflag`, only when the app has a real CLI surface**:
  subcommands, flags a human types, anything beyond "read env vars and
  serve." As a language server, this repo's primary interface is stdio/LSP,
  not flags; keep flag surface minimal (e.g. a `--stdio`/`--version`
  switch) and wire it through `github.com/spf13/cobra` rather than a bare
  `pflag.FlagSet` if it grows past a couple of flags (see `chaski`, `flate`,
  `ocharted`).

## Project layout

`cmd/<app>/main.go` is the entrypoint; everything else lives under
`internal/` unless another repo needs to import it as a library
(`github.com/home-operations/flate` is imported directly by this repo, and
by `downflate`/`konflate`, which is why it isn't under any of their
`internal/`). Keep `main.go` to wiring: parse config/flags, build the
logger, construct the LSP handler, run, translate the top-level error into
an exit code. Business logic (parsing, linting rules) belongs in
`internal/<package>`, not in `main`.

## Errors

Wrap with `fmt.Errorf("<component>: %w", err)` so a caller gets context
without losing the original error for `errors.Is`/`errors.As`/`errors.AsType`.
The `"config: %w"`-style prefix used throughout this fleet is deliberate:
grep an error message and you know which package raised it. A parse/lint
failure on user input is not the same as an internal bug: surface the
former as an LSP diagnostic on the document, not as a returned Go error
that tears down the session; reserve Go-level errors for things the client
can't route through diagnostics (e.g. transport failures). Never discard an
error silently: `_ = someCall()` is only for genuinely fire-and-forget
calls, and say why in a comment when that's not obvious.

## Context & shutdown

Every function that does I/O (including a lint pass over a large document,
if it can take a while) takes a `context.Context` as its first parameter,
so an LSP client cancelling a request (`$/cancelRequest`) can actually stop
work in flight. Long-running processes derive their root context from
`signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)`.
Prefer `golang.org/x/sync/errgroup` over raw `sync.WaitGroup` + channels
when fanning out goroutines that can fail.

## Build, lint, test (via mise)

Mise is mandatory: it pins the exact Go and golangci-lint versions
(`.mise/config.toml`), so running `go build`/`go test` outside `mise run`
risks a toolchain mismatch with CI. `mise tasks` lists everything available
in this repo:

```bash
mise run build       # go build ./...
mise run fmt          # go fmt ./...
mise run fmt-files    # format non-Go fixture/test files, if applicable
mise run vet          # go vet ./...
mise run test         # go test -race ./... -coverprofile cover.out
mise run lint         # golangci-lint run
mise run lint-fix     # golangci-lint run --fix
mise run run          # run the language server locally
mise run zizmor       # GitHub Actions workflow security linting
```

`lefthook` (`.lefthook.toml`, extending the shared `home-operations/.github`
config) runs `gofmt -s -w` on staged `.go` files pre-commit. CI additionally
fails on `go mod tidy` producing a diff; run it locally before pushing.
Lint rules live in `.golangci.yml`; read it instead of trusting a restated
list here, since the two can drift.

## Containers

Static binaries by default: `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X
main.version=... -X main.commit=..."`, built `FROM golang:<pinned>-alpine`
and run `FROM gcr.io/distroless/static:nonroot` (see `ocharted/Dockerfile`
for a reference implementation), though it's less central here since this
is primarily consumed as a local editor tool/binary rather than a
long-running containerized service; the release build should still follow
it. Only drop `CGO_ENABLED=0` if a dependency genuinely requires cgo.

## Security

`govulncheck ./...` (`go install golang.org/x/vuln/cmd/govulncheck@latest`)
isn't wired into CI anywhere in the fleet yet. Run it before cutting a
release regardless, and consider proposing it as a `mise run` task / CI job
here.
