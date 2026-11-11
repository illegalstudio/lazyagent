---
title: "Linux GUI"
description: "The Linux system-tray and desktop app, including package formats and runtime requirements."
sidebar:
  order: 3
---

```bash
lazyagent --gui
```

The Linux desktop build runs Lazyagent in the system tray. Click its icon to toggle the attached panel; detach the panel to open the full dashboard. The packaged application uses the same executable for the GUI, TUI, subcommands, and HTTP API.

## Install

Every GitHub release provides four desktop artifacts per architecture, for `amd64` and `arm64`:

- `.deb` for Debian, Ubuntu, and derivatives
- `.rpm` for Fedora and derivatives with WebKitGTK 4.1
- `.pkg.tar.zst` for Arch Linux and derivatives, also served from the [Illegal Studio pacman repository](https://illegalstudio.github.io/pacman/)
- `.AppImage` as a portable fallback

Native packages are preferred because they install the launcher, icon, AppStream metadata, CLI command, and correct GTK/WebKit runtime dependencies. See [Installation](../getting-started/installation.md#linux-desktop-app) for commands.

The build currently targets GTK3 and WebKitGTK 4.1. Native packages install or require these runtime libraries automatically. An AppImage bundles the application stack and can be run after making it executable.

## Terminal and editor actions

For Resume and terminal `$EDITOR` actions, the default `terminal` setting follows the desktop preference via `xdg-terminal-exec` when available. Lazyagent then falls back to the Debian alternatives entry and common emulators including Kitty, Ghostty, WezTerm, Alacritty, GNOME Console/Terminal, Konsole, XFCE Terminal, and xterm. Selecting `kitty` in Settings forces Kitty.

Resume actions are available in normal and YOLO variants from session cards,
the context menu, and session details. YOLO applies the agent-specific
permissive flag. pi only exposes the normal action.

GUI editors are still launched directly from the configured `editor` command or `$VISUAL`.

## Cursor cloud agents

The attached panel and detached dashboard both show Cursor cloud runs alongside local sessions. Open **Settings** (the gear icon in either layout), then **Cursor cloud agents** to create, save, replace, or remove a Cursor API key. The password field never displays the saved value. Keys share the [same credential file as the CLI and API](../reference/configuration.md#provider-api-credentials). Changes apply to this GUI immediately; restart other running instances. An environment override takes precedence and remains active after removing a saved key.

Cloud rows and cards carry a **cloud** badge. Select one for its repository, update time, and URL; **Open in Cursor** opens the run in your browser and **Copy cloud URL** copies its link. Local terminal, editor, and YOLO actions are replaced by cloud actions. Run conversations, files, and pull request details stay in Cursor.

The cloud status bar reports background refreshes, count, and errors while local sessions remain usable. **Idle cloud** (or **a**) includes completed and idle runs of any age. Active runs always remain eligible regardless of their last update time. Search and activity filters apply to both sources; the time window applies to local sessions. The refresh toolbar button also refreshes cloud data immediately. Without a key, or with Cursor excluded from the monitored agents, the lists stay local. Demo mode never fetches cloud data.

## Tray compatibility

Lazyagent exposes a StatusNotifierItem tray icon. KDE Plasma, many panels, and Ubuntu's GNOME session support it directly. A vanilla GNOME Shell installation may require an AppIndicator/KStatusNotifierItem extension; without a tray host the background process can run without a visible icon.

The icon follows the desktop portal's light/dark color-scheme preference. If the portal does not expose a preference, Lazyagent assumes a dark panel.

## Build packages locally

Install Go, Node.js, Wails 3, `pkg-config`, the GTK3 development headers, and WebKitGTK 4.1 development headers, then run:

```bash
make linux-packages VERSION=0.13.6
```

Artifacts are written under `dist/`. Release CI builds them on Ubuntu 22.04 to keep the glibc baseline compatible with newer supported distributions.
