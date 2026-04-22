---
title: "Editor support"
description: "How lazyagent picks an editor when you open a session's working directory."
sidebar:
  order: 1
---

Pressing <kbd>o</kbd> in the [Terminal UI](../interfaces/terminal-ui.md) or clicking **Open** in the [macOS GUI](../interfaces/macos-gui.md) opens the selected session's CWD in your editor. The resolution order is:

1. **Cursor-specific shortcut** — if the selected session is a Cursor session *and* the `cursor` CLI is installed, it opens in Cursor IDE directly. If `cursor` is missing, the standard flow below is used.
2. **`$VISUAL` / `$EDITOR`** — picked based on what's set, with a TUI popup when both are defined.
3. **Hint** — if neither is set, lazyagent shows a short message pointing at how to configure them.

## Resolution table

| Configuration | Behavior |
|---------------|----------|
| Both `$VISUAL` and `$EDITOR` set | Picker popup asks which one to use (TUI only; GUI defaults to `$VISUAL`) |
| Only `$VISUAL` set | Opens as GUI editor |
| Only `$EDITOR` set | Opens as TUI editor (suspends the TUI while the editor runs) |
| Neither set | Hint on how to configure them |

## Recommended setup

Add to your shell rc file:

```bash
# ~/.zshrc or ~/.bashrc
export VISUAL="code"   # GUI editor: VS Code, Cursor, Zed, …
export EDITOR="nvim"   # TUI editor: vim, nvim, nano, …
```

With both set, the TUI will ask which to use each time — useful for switching between a GUI quick-jump and a full terminal session.

## Config override

You can force a specific editor via [`editor`](configuration.md) in `config.json`:

```json
{ "editor": "code" }
```

When set, this overrides `$VISUAL` / `$EDITOR` for all open actions.
