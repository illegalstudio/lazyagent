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

- [ ] **0.1** Create `internal/core/` package
- [ ] **0.2** Move `watcher.go` → `internal/core/watcher.go`
  - Rename package to `core`
  - Export `ProjectWatcher` struct and `Events` channel
  - No behavior change
- [ ] **0.3** Move activity state machine → `internal/core/activity.go`
  - Move `ActivityKind`, `activityColors`, `activityEntry`, `resolveActivity()`, `isActiveActivity()`
  - Move timeout constants (`activityTimeout`, `idleTimeout`, etc.)
  - Export what the TUI and app both need
- [ ] **0.4** Move shared helpers → `internal/core/helpers.go`
  - `formatDuration()`, `formatTokens()`, `formatCost()`, `estimateCost()`, `shortName()`
  - These are used by both TUI rendering and will be used by frontend via Go bindings
- [ ] **0.5** Create `internal/core/session.go` — session manager
  - `SessionManager` struct: wraps `claude.DiscoverSessions()` + `ProjectWatcher` + activity tracking
  - Methods:
    - `New() *SessionManager`
    - `Start()` — starts watcher, background reload loop
    - `Stop()`
    - `Sessions() []SessionView` — returns current state (filtered, sorted)
    - `SessionDetail(id string) *SessionDetailView` — single session full info
  - `SessionView` — lightweight struct for list display (ID, name, activity, sparkline data, cost)
  - `SessionDetailView` — full struct for detail panel (all fields from `claude.Session` + computed activity)
- [ ] **0.6** Create `internal/core/config.go` — config system
  - `Config` struct:
    ```go
    type Config struct {
        WindowMinutes  int    `yaml:"window_minutes"`   // default: 60
        DefaultFilter  string `yaml:"default_filter"`   // default: "" (all)
        Editor         string `yaml:"editor"`           // override $VISUAL/$EDITOR
        LaunchAtLogin  bool   `yaml:"launch_at_login"`  // macOS only
        Notifications  bool   `yaml:"notifications"`    // notify on waiting state
        NotifyAfterSec int    `yaml:"notify_after_sec"` // seconds before notifying (default: 30)
    }
    ```
  - `LoadConfig()` — reads `~/.config/lazyagent/config.yaml`, creates default if missing
  - `SaveConfig()` — writes back (for runtime changes like launch_at_login toggle)
  - Dependency: `gopkg.in/yaml.v3` (ask permission before adding)
- [ ] **0.7** Update `internal/ui/app.go` to import from `core`
  - Replace local activity/watcher code with `core.` imports
  - Load config at startup, use `Config.WindowMinutes` as default
  - TUI must still work identically
- [ ] **0.8** Move `main.go` → `cmd/tui/main.go`, keep root `main.go` as alias
- [ ] **0.9** Verify: `go build ./cmd/tui/` works, `go build .` works, all behavior unchanged

### Verification
```bash
go build ./cmd/tui/
go build .
# Run TUI, verify all features work: sparkline, cost, spinner, filter, search, open
```

---

## Phase 1 — Wails v3 Project Scaffold

- [ ] **1.1** Install Wails v3 CLI
  ```bash
  go install github.com/wailsapp/wails/v3/cmd/wails3@latest
  ```
- [ ] **1.2** Initialize Wails project in `cmd/app/`
  - Use Svelte + TypeScript template
  - Configure `wails.json` with correct paths
- [ ] **1.3** Configure `frontend/` with Svelte 5 + Tailwind 4
  - `package.json`: svelte 5, @sveltejs/vite-plugin-svelte, tailwindcss 4
  - `app.css`: `@import "tailwindcss"` + `@theme { ... }` with lazyagent color palette
  - `vite.config.ts`: Svelte + Wails plugin
- [ ] **1.4** Set up macOS-specific config
  - `build/darwin/Info.plist`: bundle ID `com.oltrematica.lazyagent`, LSUIElement=true
  - `build/darwin/icons.icns`: app icon (can reuse/adapt existing asset)
  - `ActivationPolicyAccessory` in Go code
- [ ] **1.5** Verify: `wails3 dev` starts, shows empty Svelte app with tray icon

### Verification
```bash
cd cmd/app && wails3 dev
# Tray icon appears in menu bar, no Dock icon, click shows empty panel
```

---

## Phase 2 — Go Backend Service

The bridge between `internal/core/` and the Svelte frontend.

- [ ] **2.1** Create `cmd/app/service.go` — `SessionService` struct
  ```go
  type SessionService struct {
      manager *core.SessionManager
  }
  ```
- [ ] **2.2** Expose methods (auto-bound to JS):
  - `GetSessions() []core.SessionView` — list for sidebar
  - `GetSessionDetail(id string) *core.SessionDetailView` — full detail
  - `GetActivityTimeline(id string) []TimelineBucket` — pre-bucketed sparkline data
  - `OpenInEditor(cwd string)` — reuse editor-open logic
  - `GetConfig() AppConfig` — window minutes, filter, etc.
  - `SetWindowMinutes(m int)`
  - `SetActivityFilter(f string)`
- [ ] **2.3** Push updates to frontend via Wails events:
  - `sessions:updated` — emitted when watcher detects changes or periodic refresh
  - `activity:changed` — emitted when a session's activity state changes
  - Frontend subscribes to events for reactive updates
- [ ] **2.4** Create `cmd/app/main.go`:
  ```go
  func main() {
      app := application.New(application.Options{
          Name: "lazyagent",
          Mac: application.MacOptions{
              ActivationPolicy: application.ActivationPolicyAccessory,
          },
          Services: []application.Service{
              application.NewService(&SessionService{...}),
          },
      })
      tray := app.SystemTray.New()
      window := app.Window.NewWithOptions(...)
      tray.AttachWindow(window)
      app.Run()
  }
  ```
- [ ] **2.5** Generate TypeScript bindings: `wails3 generate bindings`
- [ ] **2.6** Verify: call `GetSessions()` from browser console in dev mode, get real data

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

- [ ] **3.1** `stores.ts` — Svelte stores + Wails event listeners
  - `sessions` writable store, updated on `sessions:updated` event
  - `selectedId` writable store
  - `windowMinutes` writable store (synced with Go)
  - `activityFilter` writable store
- [ ] **3.2** `App.svelte` — root layout
  - Header bar with title, active count, time window control
  - Session list
  - Detail panel (shown when session selected)
  - Keyboard shortcuts (j/k navigate, Enter select, Esc back)
- [ ] **3.3** `SessionList.svelte` — scrollable list
  - Each row: activity dot (colored), project name, sparkline, activity badge
  - Click to select, highlight selected row
  - Search input (filter by name)
- [ ] **3.4** `SessionDetail.svelte` — detail view
  - All info from TUI detail panel, styled for GUI
  - "Open in Editor" button (calls `OpenInEditor`)
  - Conversation preview (scrollable)
  - Tool history
- [ ] **3.5** `Sparkline.svelte` — SVG sparkline
  - SVG-based (not Unicode) — smoother, anti-aliased, resizable
  - Receives bucketed data from Go, renders as filled area chart
  - Color matches activity state
- [ ] **3.6** `ActivityBadge.svelte` — status pill
  - Colored badge with activity label
  - Animated pulse for active states (replaces TUI spinner)
- [ ] **3.7** Tailwind theme in `app.css`
  ```css
  @import "tailwindcss";
  @theme {
    --color-surface: #1e1e2e;
    --color-surface-hover: #313244;
    --color-text: #cdd6f4;
    --color-subtext: #a6adc8;
    --color-accent: #cba6f7;
    --color-border: #45475a;
    /* Activity colors matching TUI palette */
    --color-activity-thinking: #89b4fa;
    --color-activity-writing: #a6e3a1;
    --color-activity-reading: #f9e2af;
    --color-activity-running: #fab387;
    --color-activity-waiting: #6c7086;
    --color-activity-idle: #585b70;
    --color-activity-searching: #74c7ec;
    --color-activity-browsing: #94e2d5;
    --color-activity-spawning: #f38ba8;
    --color-activity-compacting: #cba6f7;
  }
  ```
- [ ] **3.8** Verify: full UI renders with live data, updates reactively

### Verification
```bash
wails3 dev
# Panel shows real sessions, sparklines animate, clicking shows detail
# Press j/k to navigate, search filters, click "Open in Editor" works
```

---

## Phase 4 — System Tray Polish

- [ ] **4.1** Tray icon design
  - Template icon (macOS-native, adapts to light/dark menu bar)
  - 16x16 and 32x32 @2x versions
  - Badge: show number of active sessions as overlay or in title
- [ ] **4.2** Tray icon dynamic updates
  - Change icon when sessions are active vs all idle
  - Option: show count in tray title (`tray.SetTitle("3")`)
- [ ] **4.3** Right-click context menu
  - "Show Panel" / "Hide Panel"
  - "Refresh Now"
  - Separator
  - List of active sessions (click → open in editor)
  - Separator
  - "Quit lazyagent"
- [ ] **4.4** Window behavior
  - `HideOnFocusLost: true` — panel disappears when clicking away
  - `HideOnEscape: true` — Esc closes panel
  - Arrow points to tray icon (native macOS popover style)
  - Remember last panel size
- [ ] **4.5** macOS notifications (via `UserNotifications` framework or `osascript`)
  - Notify when session enters "waiting" state for > `Config.NotifyAfterSec` seconds
  - Configurable on/off via `Config.Notifications`
  - Notification click → show panel + select session
- [ ] **4.6** Launch at Login
  - Toggle in context menu: "Launch at Login" (checkmark)
  - Persisted in `Config.LaunchAtLogin`
  - Implementation: `SMAppService.register` (modern macOS) or `launchd` plist in `~/Library/LaunchAgents/`
  - Plist points to `/Applications/lazyagent.app`
- [ ] **4.7** Verify: tray icon updates, context menu works, panel shows/hides, launch at login toggles

---

## Phase 5 — Build & Distribution

- [ ] **5.1** Build `.app` bundle
  ```bash
  cd cmd/app && wails3 build -platform darwin/arm64
  ```
  - arm64-only (Apple Silicon)
  - Output: `build/bin/lazyagent.app`
- [ ] **5.2** Code signing
  - Set up `gon` config for Developer ID signing
  - Ad-hoc signing for dev builds
- [ ] **5.3** Notarization
  - `gon` config for Apple notarization
  - Required for distribution outside App Store
- [ ] **5.4** DMG creation
  - Background image, app icon + Applications folder shortcut
  - Signed DMG via `gon`
- [ ] **5.5** Homebrew cask
  ```ruby
  cask "lazyagent-app" do
    version "0.3.0"
    sha256 "..."
    url "https://github.com/illegalstudio/lazyagent/releases/download/v#{version}/lazyagent-#{version}.dmg"
    name "lazyagent"
    homepage "https://github.com/illegalstudio/lazyagent"
    app "lazyagent.app"
  end
  ```
- [ ] **5.6** GoReleaser integration
  - Add Wails build step to `.goreleaser.yaml`
  - Produce both CLI binary and `.app` in releases
- [ ] **5.7** GitHub Actions CI
  - Build TUI (all platforms) + macOS app
  - Sign + notarize on tag push
  - Upload to GitHub Releases
- [ ] **5.8** Verify: download DMG, drag to Applications, launch, tray icon appears

---

## Phase 6 — Integration & QA

- [ ] **6.1** Both entry points build from same `go.mod`
  ```bash
  go build -o lazyagent ./cmd/tui           # TUI
  wails3 build -o lazyagent-app ./cmd/app   # macOS app
  ```
- [ ] **6.2** Feature parity checklist (app vs TUI):
  - [ ] Session list with activity states
  - [ ] Sparkline activity graph
  - [ ] Token usage & cost estimation
  - [ ] Filter by activity type
  - [ ] Search by project name
  - [ ] Time window control
  - [ ] Open in editor
  - [ ] Detail panel: model, branch, messages, tools, conversation
  - [ ] Auto-refresh on file changes
- [ ] **6.3** Performance: panel opens in < 100ms, updates in < 50ms
- [ ] **6.4** Memory: < 30MB RSS at idle with 20 sessions
- [ ] **6.5** README update: document both install methods (TUI vs app)

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
