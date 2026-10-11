# Čysta Dyska

Windows desktop app that finds what uses disk space, explains who owns it, and offers safe ways
to clean it up. Go backend + Wails v2 + React/Vite/shadcn frontend. Windows only (non-Windows
builds compile via stubs so `go vet` works cross-platform).

## Commands

```bash
go test -race -count=1 ./...                  # all Go tests (needs gcc for -race; MSYS2 is installed)
go vet ./... && CGO_ENABLED=0 GOOS=linux go vet ./internal/...   # CGO_ENABLED=0 or cgo fails with MinGW
cd frontend && npx tsc -b && npm run lint     # type check + oxlint
wails build                                   # -> build/bin/cysta-dyska.exe
wails dev -browser=false                      # live app; bindings also work in a browser at http://localhost:34115
wails generate module                         # regenerate frontend/wailsjs after changing bound Go methods/DTOs
go run ./cmd/cysta-scan -culprits -top 30 'C:\Users\me'   # headless scan + attribution, for debugging/benchmarks
CYSTA_RECYCLE_TEST=1 go test -run RecycleReal ./internal/actions/   # opt-in: uses the REAL Recycle Bin
```

- `frontend/dist` is gitignored but embedded by `main.go`; on a fresh clone build the frontend
  (`npm run build` or `wails build`) before `go vet`/`go build` of the root package.
- The Wails CLI must match `go.mod` (v2.16.0) and be built with the current Go; an older CLI fails
  with `package "context" without types`.

## Layout

- `main.go` – Wails bootstrap, `Shell` (folder picker, Reveal, OpenSettings), opens the history DB.
- `internal/scan` – parallel walker (`GetFileInformationByHandleEx` batches): on-disk size,
  hardlinks counted once (deterministic, name order), name-surrogate reparse points not followed.
- `internal/attribute` – embedded YAML rule pack (`rules/*.yaml`) + matcher → findings.
- `internal/sources/locations` – resolves `{VAR}` folders (env, `go env -w` file, `.npmrc`); never runs tools.
- `internal/sources/registry` – installed programs from the Uninstall keys, **read-only**.
- `internal/store` – SQLite history (`%LOCALAPPDATA%\CystaDyska\history.db`, modernc, pure Go):
  snapshots, settings, action log; stepwise `migrations` (never edit a shipped one, append).
- `internal/treemap` – squarified layout (computed in Go so it is testable).
- `internal/actions` – path Guard, target measurement, Recycle Bin, delete, commands, UAC, uninstaller.
- `internal/app` – the Wails-bound facade; DTOs; plan/run of actions.
- `internal/guard` – source-level tests enforcing project rules (no registry writes).
- `frontend/src/features/*` – one folder per view (culprits, scan, map, history, activity, settings).

## Safety invariants (data safety is the top requirement)

- The registry is never written. `internal/guard` fails on any registry write API.
- Nothing changes on disk without PlanAction → user confirmation → RunAction, and RunAction
  re-checks everything (guard, fresh measure, setting) right before acting. A plan runs once.
- File actions only on rules that opt in (`fileAction`), never on `system`/`never` risk or
  installed programs, never through `actions.Guard` refusals, never on targets containing links.
- Permanent delete is off by default (setting `allow_permanent_delete`). Recycle Bin is the default.
- Commands: exact program + args from the rule's `run` block, no shell, no stdin. The displayed
  `command` must equal the run command (validated). Admin programs only from `System32`.
- Name rules (node_modules, target, bin/obj, .venv) never match inside claimed folders, app areas
  (AppData, ProgramData, Program Files, Windows) or home dot-folders.
- Tests never touch the real Recycle Bin or run real tools: `App.exec` executors are faked.
  Don't run cleanup commands against the user's real caches to "test" them.

## Conventions

- New behaviour starts with a test; bug fixes start with a failing test. Each test has a
  `// Proves:` comment. For safety checks, verify the test fails when the check is removed.
- DTO slices sent to the frontend must be non-nil (`[]`, never JSON `null`) — a null list once
  crashed the UI into a black window.
- Windows struct mirrors (`FILE_ID_BOTH_DIR_INFO`, `SHFILEOPSTRUCTW`, `SHELLEXECUTEINFOW`) have
  layout tests; check Win32 APIs against learn.microsoft.com, not memory.
- Rule pack: strict YAML (`KnownFields`); every text needs `en` and `be`; Windows paths in
  double-quoted YAML need `\\`. `go test ./internal/attribute` validates the shipped pack.
- i18n: all UI strings in `frontend/src/i18n/en.ts` and `be.ts` (Belarusian, Cyrillic; `be.ts` is
  type-checked against `en.ts`). Belarusian text needs native review.
- Frontend: shadcn on Radix (`npx shadcn@latest add <c>`); class merging uses the `cn` package
  (`import { cn } from 'cn'`, intended). `erasableSyntaxOnly` is off because Wails' generated
  `models.ts` uses namespaces. Fully offline: no network calls, fonts bundled.
- Git has `core.autocrlf=true`: after checkouts `gofmt -l` lists CRLF files that are fine in the
  repo; compare with `git diff --ignore-cr-at-eol`. Work on a `mN-*` branch per milestone,
  fast-forward `main` when asked. Don't commit or push unless asked.

## Status

Milestones M1–M5 are done (scan, on-disk sizes, attribution, history/treemap, confirmed actions).
Next: M6 – optional elevated MFT fast scan, installer (NSIS), code signing (undecided),
crash-safe scan resume.
