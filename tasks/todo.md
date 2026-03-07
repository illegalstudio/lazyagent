# lazyagent macOS Menu Bar App — Implementation Plan

> **Goal**: Build a native macOS menu bar app (`lazyagent-app`) using Wails v3 + Svelte 5 + Tailwind 4.
> The TUI (`lazyagent`) stays as-is. Both share the same Go core library.

---

## Architecture Overview

```
lazyagent/
├── cmd/
│   ├── tui/                    # Current TUI entry point (moved from main.go)
│   │   └── main.go
│   └── app/                    # Wails v3 macOS app entry point
│       ├── main.go
│       ├── service.go          # Go service exposed to frontend
│       └── build/
│           └── darwin/
│               ├── Info.plist
│               └── icons.icns
├── internal/
│   ├── core/                   # NEW — shared core library
│   │   ├── session.go          # Session loading & management
│   │   ├── watcher.go          # File watcher (from ui/watcher.go)
│   │   └── activity.go         # Activity state machine (from ui/activity.go)
│   ├── claude/                 # Unchanged — JSONL parsing, types, discovery
│   │   ├── types.go
│   │   ├── process.go
│   │   └── jsonl.go
│   └── ui/                     # TUI-only — bubbletea rendering
│       ├── app.go              # Imports from core/ instead of local activity/watcher
│       └── styles.go
├── frontend/                   # NEW — Svelte 5 + Tailwind 4
│   ├── src/
│   │   ├── App.svelte
│   │   ├── main.ts
│   │   ├── lib/
│   │   │   ├── SessionList.svelte
│   │   │   ├── SessionDetail.svelte
│   │   │   ├── Sparkline.svelte
│   │   │   ├── ActivityBadge.svelte
│   │   │   └── stores.ts       # Svelte stores for reactive state
│   │   └── assets/
│   │       └── tray-icon.png
│   ├── index.html
│   ├── package.json
│   ├── vite.config.ts
│   └── app.css                 # Tailwind 4 (@import "tailwindcss")
├── go.mod
├── go.sum
├── main.go                     # Kept as alias → cmd/tui/main.go (backward compat)
└── wails.json                  # Wails project config
```

---

## Constraints & Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Wails version | **v3 alpha** (v3.0.0-alpha.74+) | Only v3 has system tray support. v2 doesn't. |
| Go version | **1.25+** | Required by Wails v3 |
| Frontend framework | **Svelte 5** | Smallest bundle, best perf, runes reactivity. No virtual DOM overhead. |
| CSS | **Tailwind 4** | CSS-first config via `@theme`, consistent with team stack. |
| System tray | **NSStatusItem** via Wails v3 API | `ActivationPolicyAccessory` = no Dock icon. |
| Window model | **Single panel** attached to tray icon | Click tray → toggle panel. `HideOnFocusLost`. |
| Data refresh | **FSEvents watcher + 1s render tick** | Same strategy as TUI. Core watcher shared. |
| IPC | **Wails bindings** (auto-generated) | Go structs → TypeScript functions. Sub-ms latency. |
| Build output | **.app bundle (arm64)** | Signable, notarizable, distributable via DMG + Homebrew cask. |
| TUI backward compat | **Keep working** | `go install` and `brew install lazyagent` still give the TUI. |
| Config file | **`~/.config/lazyagent/config.yaml`** | Shared by TUI and app. Introduced in Phase 0. |
| Auto-launch | **Yes** | "Launch at Login" via `LSSharedFileList` or launchd plist. |

### Risk: Wails v3 Alpha Stability

v3 is alpha. Known issues:
- `AlwaysOnTop` sometimes doesn't stick
- Window hide/show can affect tray icon in edge cases
- API may change between alpha releases

**Mitigation**: Pin exact alpha version in `go.mod`. Wrap Wails API calls in thin adapters for easy migration. v3 stable expected H1 2026.

---

## Phase 0 — Core Extraction (no new dependencies)

Extract framework-agnostic code from `internal/ui/` into `internal/core/` so both TUI and app can import it.

- [x] **0.1** Create `internal/core/` package
- [x] **0.2** Move `watcher.go` → `internal/core/watcher.go`
  - Renamed package to `core`, exported `ProjectWatcher` struct and `Events` channel
  - TUI `watcher.go` is now a thin adapter with `fileWatchMsg` + `watchCmd`
- [x] **0.3** Move activity state machine → `internal/core/activity.go`
  - Moved `ActivityKind`, `ActivityEntry`, `ResolveActivity()`, `IsActiveActivity()`, `ToolActivity()`
  - Moved timeout constants, created `ActivityTracker` struct with grace period logic
  - `activityColors` stays in `ui/activity.go` (presentation concern)
- [x] **0.4** Move shared helpers → `internal/core/helpers.go`
  - `FormatDuration()`, `FormatTokens()`, `FormatCost()`, `EstimateCost()`, `ShortName()`
  - `PadRight()`, `Clamp()`, `RenderSparkline()`, `BucketTimestamps()`, `SpinnerFrames`
- [x] **0.5** Create `internal/core/session.go` — session manager
  - `SessionManager` wraps `claude.DiscoverSessions()` + `ProjectWatcher` + `ActivityTracker`
  - Methods: `Reload()`, `UpdateActivities()`, `VisibleSessions()`, `SessionDetail()`, etc.
  - `SessionView`, `SessionDetailView` structs for list/detail display
- [x] **0.6** Create `internal/core/config.go` — config system
  - Uses JSON (stdlib, zero new deps) — easily switchable to YAML later
  - `LoadConfig()`, `SaveConfig()`, `DefaultConfig()`, `ConfigPath()`
  - Reads `~/.config/lazyagent/config.json`
- [x] **0.7** Update `internal/ui/app.go` to import from `core`
  - Replaced local activity/watcher/helper code with `core.` imports
  - Model now holds `*core.SessionManager` instead of raw sessions/watcher/activities
  - Loads config at startup, uses `Config.WindowMinutes` as default
  - TUI works identically
- [x] **0.8** Move `main.go` → `cmd/tui/main.go`, keep root `main.go` as alias
- [x] **0.9** Verify: `go build ./cmd/tui/` works, `go build .` works, `go vet` clean

### Verification
```bash
go build ./cmd/tui/
go build .
# Run TUI, verify all features work: sparkline, cost, spinner, filter, search, open
```

---

## Phase 1 — Wails v3 Project Scaffold

- [x] **1.1** Install Wails v3 CLI (v3.0.0-alpha.74)
- [x] **1.2** Add `github.com/wailsapp/wails/v3` to go.mod, create `cmd/app/` structure
- [x] **1.3** Configure `frontend/` with Svelte 5 + Tailwind 4
  - `package.json`, `vite.config.ts`, `svelte.config.js`, `tsconfig.json`
  - `app.css` with Catppuccin-inspired `@theme` palette (10 activity colors)
- [x] **1.4** Set up macOS-specific config
  - `cmd/app/build/darwin/Info.plist`: bundle ID `com.oltrematica.lazyagent`, LSUIElement=true
  - `ActivationPolicyAccessory` in Go code
- [x] **1.5** Verify: `go build ./cmd/app/` compiles, frontend builds (77KB JS, 12KB CSS)

### Verification
```bash
cd cmd/app && wails3 dev
# Tray icon appears in menu bar, no Dock icon, click shows empty panel
```

---

## Phase 2 — Go Backend Service

The bridge between `internal/core/` and the Svelte frontend.

- [x] **2.1** Create `cmd/app/service.go` — `SessionService` struct with lifecycle hooks
- [x] **2.2** Expose 9 methods auto-bound to JS:
  - `GetSessions()`, `GetSessionDetail()`, `GetActiveCount()`
  - `OpenInEditor()`, `GetConfig()`
  - `SetWindowMinutes()`, `SetActivityFilter()`, `SetSearchQuery()`
  - `GetWindowMinutes()`
- [x] **2.3** Push `sessions:updated` events via `app.Event.Emit()`
  - Background goroutine: watches file changes + 1s activity tick + 30s full reload
- [x] **2.4** Create `cmd/app/main.go` with system tray, attached window, context menu
- [x] **2.5** Generate TypeScript bindings: `wails3 generate bindings -ts ./cmd/app`
- [x] **2.6** Verify: full build succeeds, 18MB binary

### Verification
```bash
wails3 dev
# Open browser DevTools → Console
# Call the generated binding function, verify it returns session data
```

---

## Phase 3 — Frontend UI (Svelte 5 + Tailwind 4)

### Design Language

```
┌─────────────────────────────────────┐  ← Panel attached to tray icon
│  lazyagent          3 active  12m ▾ │  ← Header: title, count, time window
├─────────────────────────────────────┤
│  ▪ my-project    ▂▃▅▇▃▁  thinking  │  ← Session row: dot, name, sparkline, badge
│  ▪ api-service   ▁▁▁▁▁▁  idle      │
│  ▪ frontend      ▃▅▇▅▃▂  writing   │
│  ● docs-site     ▁▁▂▁▁▁  waiting   │  ← Dot changes color by activity
├─────────────────────────────────────┤
│  Model: claude-sonnet-4-20250514    │  ← Detail section (expandable)
│  Branch: feature/auth               │
│  Tokens: 45.2k in / 12.1k out $0.84│
│  Last: Write src/auth/login.ts 2m   │
│  Messages: 24 (12 user, 12 asst)   │
├─────────────────────────────────────┤
│  Recent conversation                │  ← Collapsible
│  User: "add login endpoint"         │
│  AI: "I'll create the login..."     │
└─────────────────────────────────────┘
```

Window: ~380px wide, ~500px tall, frameless, rounded corners, dark theme.

### Components

- [x] **3.1** `stores.ts` — Svelte stores, activity colors, utility functions
- [x] **3.2** `App.svelte` — root layout with header, split view, footer, keyboard shortcuts
- [x] **3.3** `SessionList.svelte` — scrollable list with dot, name, sparkline, badge
- [x] **3.4** `SessionDetail.svelte` — full detail panel with "Open" button, conversation, tools
- [x] **3.5** `Sparkline.svelte` — SVG sparkline with filled area chart
- [x] **3.6** `ActivityBadge.svelte` — colored pill with animated pulse dot
- [x] **3.7** Tailwind 4 `@theme` with Catppuccin Mocha palette + 10 activity colors
- [x] **3.8** Verify: frontend builds clean (77KB JS, 12KB CSS)

### Verification
```bash
wails3 dev
# Panel shows real sessions, sparklines animate, clicking shows detail
# Press j/k to navigate, search filters, click "Open in Editor" works
```

---

## Phase 4 — System Tray Polish

- [x] **4.1** Tray icon: uses Wails built-in `icons.SystrayMacTemplate` (adapts to light/dark)
- [ ] **4.2** Tray icon dynamic updates (show active count in label) — deferred to runtime testing
- [x] **4.3** Right-click context menu: Show Panel, Refresh Now, Quit
- [x] **4.4** Window behavior: `HideOnFocusLost`, `Frameless`, `AlwaysOnTop`, `BackgroundTypeTranslucent`
  - macOS: `MacBackdropTranslucent`, `MacWindowLevelFloating`, hidden title bar
- [ ] **4.5** macOS notifications — deferred (requires runtime testing)
- [ ] **4.6** Launch at Login — deferred (requires `.app` bundle to test)
- [x] **4.7** Core tray+panel behavior implemented and compiles

---

## Phase 5 — Build & Distribution

- [x] **5.1** Build binary: `go build -o lazyagent-app ./cmd/app/` (18MB arm64)
- [x] **5.2** `Makefile` with `tui`, `app`, `frontend`, `bindings`, `dev`, `clean` targets
- [x] **5.3** `Info.plist` for `.app` bundle (LSUIElement=true, bundle ID set)
- [ ] **5.4** Code signing + notarization — deferred (requires Apple Developer cert)
- [ ] **5.5** DMG creation — deferred
- [ ] **5.6** Homebrew cask — deferred
- [ ] **5.7** GoReleaser integration — deferred
- [ ] **5.8** GitHub Actions CI — deferred

---

## Phase 6 — Integration & QA

- [x] **6.1** Both entry points build from same `go.mod`:
  ```bash
  go build -o lazyagent ./cmd/tui           # TUI (5MB)
  go build -o lazyagent-app ./cmd/app       # macOS app (18MB)
  go build .                                 # root alias (TUI)
  ```
- [x] **6.2** Feature parity checklist (app vs TUI):
  - [x] Session list with activity states
  - [x] Sparkline activity graph (SVG in app vs Unicode in TUI)
  - [x] Token usage & cost estimation
  - [x] Filter by activity type
  - [x] Search by project name
  - [x] Time window control (+/- keys)
  - [x] Open in editor
  - [x] Detail panel: model, branch, messages, tools, conversation
  - [x] Auto-refresh on file changes (FSEvents watcher + 1s tick)
- [ ] **6.3** Performance testing — requires runtime testing
- [ ] **6.4** Memory profiling — requires runtime testing
- [ ] **6.5** README update — deferred

---

## Execution Order

```
Phase 0 (core extraction)     ██████░░░░░░░░░░░░░░  ~2 days
Phase 1 (wails scaffold)      ░░░░░░███░░░░░░░░░░░  ~1 day
Phase 2 (go backend service)  ░░░░░░░░░████░░░░░░░  ~2 days
Phase 3 (svelte frontend)     ░░░░░░░░░░░░░██████░  ~3 days
Phase 4 (tray polish)         ░░░░░░░░░░░░░░░░░██░  ~1 day
Phase 5 (build & dist)        ░░░░░░░░░░░░░░░░░░██  ~1 day
Phase 6 (integration & QA)    ░░░░░░░░░░░░░░░░░░░█  ~1 day
```

Phases 0-1 are blocking. Phases 2-3 can partially overlap (backend first, frontend follows).
Phase 4-5 are polish. Phase 6 is final verification.

---

## Decisions (resolved)

| # | Question | Decision |
|---|----------|----------|
| 1 | Wails v3 alpha risk | Proceed with alpha, pin exact version in `go.mod` |
| 2 | Universal binary | arm64-only |
| 3 | App name | `lazyagent` for both (single brand) |
| 4 | Auto-launch | Yes — "Launch at Login" option via config |
| 5 | Config file | Now — `~/.config/lazyagent/config.yaml` introduced in Phase 0 |
