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
CYSTA_MFT_TEST=1 go test -run MFTMatchesWalker ./internal/scan/mft/  # opt-in, elevated shell: MFT vs walker
go run ./cmd/cysta-scan -fast 'C:\'          # "Full scan" (MFT) through the UAC helper; prints step timings
./scripts/release.ps1 [-Sign]                  # app + per-user NSIS installer in build/bin; -Sign: see scripts/sign.ps1
```

- `frontend/dist` is gitignored but embedded by `main.go`; on a fresh clone build the frontend
  (`npm run build` or `wails build`) before `go vet`/`go build` of the root package.
- `wails build -nsis` needs NSIS (`winget install NSIS.NSIS`, not on PATH by default; release.ps1
  finds it). It rewrites `build/windows/installer/wails_tools.nsh` with filled-in values: restore it
  (`git checkout`), it is generated. Edit `project.nsi` only.
- Scans run from a tool launched by the Claude desktop app see Windows' MSIX virtualization (e.g. an
  extra ~11 GB under AppData\Roaming from `Packages\Claude_*\LocalCache`). Numbers differ from a
  normally started app; don't "fix" the scanner for it.
- The Wails CLI must match `go.mod` (v2.16.0) and be built with the current Go; an older CLI fails
  with `package "context" without types`.

## Layout

- `main.go` – Wails bootstrap, `Shell` (folder picker, Reveal, OpenSettings), opens the history DB,
  single-instance lock; with `--mft-scan` it runs as the elevated fast-scan helper instead.
- `internal/scan` – parallel walker (`GetFileInformationByHandleEx` batches): on-disk size,
  hardlinks counted once (deterministic, name order), name-surrogate reparse points not followed.
- `internal/scan/mft` – NTFS MFT parser (fixups, `$FILE_NAME`, `$DATA`, WOF, extension records) and
  the raw volume reader; `scan.Build` turns its flat list into a normal scan tree.
- `internal/fastscan` – runs the MFT read in an elevated copy of the app and exchanges the result
  through a file in `%LOCALAPPDATA%\CystaDyska\fastscan`.
- `internal/attribute` – embedded YAML rule pack (`rules/*.yaml`) + matcher → findings.
- `internal/sources/locations` – resolves `{VAR}` folders (env, `go env -w` file, `.npmrc`); never runs tools.
- `internal/sources/registry` – installed programs from the Uninstall keys, **read-only**.
- `internal/store` – SQLite history (`%LOCALAPPDATA%\CystaDyska\history.db`, modernc, pure Go):
  snapshots, settings, action log; stepwise `migrations` (never edit a shipped one, append).
- `internal/treemap` – squarified layout (computed in Go so it is testable).
- `internal/actions` – path Guard, target measurement, Recycle Bin, delete, commands, UAC, uninstaller.
- `internal/app` – the Wails-bound facade; DTOs; plan/run of actions.
- `internal/guard` – source-level tests enforcing project rules (no registry writes).
- `scripts/` – `release.ps1` (build + installer, optional signing), `sign.ps1` (signtool, cert from
  the Windows store by thumbprint; no secrets in the repo).
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
- The app never runs elevated. The fast scan helper (`--mft-scan <drive> <file>`) only reads: the
  volume is opened `GENERIC_READ` and `internal/guard` allow-lists the Windows APIs in
  `internal/scan/mft`. The helper accepts only a drive letter and a new file
  `mft-<16 hex>.bin` directly in the result folder it computes itself (`O_EXCL`, never overwrites).
- The MFT scan is called "Full scan (admin)" in the UI (code still says fast scan / `fastscan`). It
  covers whole drives only, so hardlinks are counted exactly as by the walker. It also sees folders
  the walker cannot read, so snapshots carry a `method` (`walk`/`mft`) and are only listed,
  compared and pruned within the same method. Its speed is bound by MFT read throughput
  (~540 MB/s on the dev NVMe, buffered or not); parsing and tree building take ~1 s.
- Actions still "running" at startup are marked "interrupted" (single-instance lock makes that safe).
- The uninstaller deletes only the files it installed (never `RMDir /r $INSTDIR`) and keeps the
  user's history in `%LOCALAPPDATA%\CystaDyska`.
- Tests never touch the real Recycle Bin or run real tools: `App.exec` executors are faked.
  Don't run cleanup commands against the user's real caches to "test" them.

## Conventions

- New behaviour starts with a test; bug fixes start with a failing test. Each test has a
  `// Proves:` comment. For safety checks, verify the test fails when the check is removed.
- DTO slices sent to the frontend must be non-nil (`[]`, never JSON `null`) — a null list once
  crashed the UI into a black window. `append([]T(nil), empty...)` is nil too. Each view is also
  wrapped in an `ErrorBoundary`.
- Windows struct mirrors (`FILE_ID_BOTH_DIR_INFO`, `SHFILEOPSTRUCTW`, `SHELLEXECUTEINFOW`) have
  layout tests; check Win32 APIs against learn.microsoft.com, not memory.
- Rule pack: strict YAML (`KnownFields`); every text needs `en` and `be`; Windows paths in
  double-quoted YAML need `\\`. `go test ./internal/attribute` validates the shipped pack.
- i18n: all UI strings in `frontend/src/i18n/en.ts` and `be.ts` (Belarusian, Cyrillic; `be.ts` is
  type-checked against `en.ts`). Belarusian text needs native review.
- Frontend: shadcn on Radix (`npx shadcn@latest add <c>`); class merging uses the `cn` package
  (`import { cn } from 'cn'`, intended). `erasableSyntaxOnly` is off because Wails' generated
  `models.ts` uses namespaces. Fully offline: no network calls, fonts bundled.
- Edit code containing backslashes (Go raw strings, Windows paths, NSIS) with the Edit tool, not
  shell heredocs or inline Python: `\\`, `\n` and `\f` get mangled on the way.
- Git has `core.autocrlf=true`: after checkouts `gofmt -l` lists CRLF files that are fine in the
  repo; compare with `git diff --ignore-cr-at-eol`. Work on a `mN-*` branch per milestone,
  fast-forward `main` when asked. Don't commit or push unless asked.

## Status

Milestones M1–M6 are done (scan, on-disk sizes, attribution, history/treemap, confirmed actions,
MFT fast scan, crash recovery, per-user installer, optional signing). Code signing is prepared but
unused: the release is unsigned until a certificate is chosen. Version is set in `wails.json`.
