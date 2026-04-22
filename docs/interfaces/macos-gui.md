---
title: "macOS GUI"
description: "A detachable menu bar panel built with Wails v3 and Svelte 5."
sidebar:
  order: 2
---

```bash
lazyagent --gui
```

The GUI process detaches from your terminal — the shell returns immediately and the app lives in your menu bar. There's no Dock icon (it's registered as a macOS *accessory* app). Click the tray icon to toggle the panel.

![lazyagent macOS menu bar app](../../assets/tray.png)

## The detachable panel

The panel defaults to an attached popover below the menu bar icon. Press <kbd>d</kbd> (or click the detach button in the header) to pop it out into a standalone resizable window. Once detached you can:

- **Move it** anywhere on your screen.
- **Resize it** to whatever dimensions you want.
- **Pin it always-on-top** so it stays visible while you work.

Press <kbd>d</kbd> again or close the window to snap it back to the menu bar.

## Keybindings

| Key | Action |
|-----|--------|
| <kbd>↑</kbd> / <kbd>k</kbd> | Move up |
| <kbd>↓</kbd> / <kbd>j</kbd> | Move down |
| <kbd>+</kbd> / <kbd>-</kbd> | Adjust time window |
| <kbd>f</kbd> | Cycle activity filter |
| <kbd>/</kbd> | Search sessions |
| <kbd>r</kbd> | Rename session (empty resets) |
| <kbd>d</kbd> | Detach / reattach panel |
| <kbd>esc</kbd> | Close detail / dismiss search |

## Right-click menu

Right-click the tray icon for a compact menu:

- **Show Panel** — open the session panel (same as left-click)
- **Refresh Now** — force reload all sessions
- **Quit** — exit the app

## Visuals

The GUI uses Catppuccin Mocha as its theme and renders sparklines as real SVG area charts (unlike the TUI's Unicode braille). Activity badges use the same color taxonomy across all interfaces.

## Combining with other interfaces

```bash
lazyagent --gui --api            # menu bar + HTTP API
lazyagent --tui --gui --api      # everything
```

The GUI always runs in its own OS process (Cocoa requires ownership of the main thread), so combined launches fork it transparently. Quitting via the tray menu kills the tray process only — any TUI or API in the same parent invocation keeps running.
