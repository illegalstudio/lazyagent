---
title: "Installation"
description: "How to install the lazyagent desktop app or standalone CLI."
sidebar:
  order: 1
---

lazyagent ships as a desktop app for macOS and Linux plus a standalone CLI build (TUI + HTTP API, no GUI). Every package provides the same `lazyagent` command, so install either the desktop app or the CLI-only build, not both.

## Homebrew

The recommended CLI installer on macOS and Linux, and the desktop installer on macOS.

**Desktop app** (macOS, universal binary — TUI + GUI + HTTP API):

```bash
brew install --cask illegalstudio/tap/lazyagent
```

Installs `Lazyagent.app` and links the `lazyagent` command into Homebrew's bin. Installing the app zip manually instead (from GitHub releases)? Link the CLI yourself:

```bash
ln -s /Applications/Lazyagent.app/Contents/MacOS/lazyagent /usr/local/bin/lazyagent
```

**CLI only** (macOS, Linux — TUI + HTTP API, no GUI):

```bash
brew install illegalstudio/tap/lazyagent-cli
```

## Linux desktop app

Linux desktop packages are attached to each [GitHub release](https://github.com/illegalstudio/lazyagent/releases), for `amd64` and `arm64`. The package includes the graphical tray app, TUI, and HTTP API in the same `lazyagent` executable. The commands below name the `amd64` files; on arm64 substitute `arm64` (`aarch64` for the Arch package).

**Debian / Ubuntu:**

```bash
sudo apt install ./Lazyagent_VERSION_linux_amd64.deb
```

**Fedora:**

```bash
sudo dnf install ./Lazyagent_VERSION_linux_amd64.rpm
```

**Arch Linux:** install from the Illegal Studio pacman repository instead, which serves `x86_64` and `aarch64`, so upgrades arrive with `pacman -Syu`. Add this at the bottom of `/etc/pacman.conf`:

```ini
[illegalstudio]
SigLevel = Optional TrustAll
Server = https://illegalstudio.github.io/pacman/$arch
```

```bash
sudo pacman -Syu lazyagent
```

The repository is unsigned, hence `SigLevel = Optional TrustAll`, and it keeps only the latest version — older ones stay on the [releases page](https://github.com/illegalstudio/lazyagent/releases), installable by hand:

```bash
sudo pacman -U ./lazyagent-VERSION-1-x86_64.pkg.tar.zst
```

The native packages install the application-menu launcher, icon, AppStream metadata, and `lazyagent` on `PATH`. For other distributions, download the AppImage:

```bash
chmod +x Lazyagent_VERSION_linux_amd64.AppImage
./Lazyagent_VERSION_linux_amd64.AppImage
```

See [Linux GUI](../interfaces/linux-gui.md) for runtime dependencies and tray compatibility notes.

## Go (TUI only)

If you only need the terminal interface and already have a Go toolchain:

```bash
go install github.com/illegalstudio/lazyagent@latest
```

`go install` names the binary after the module path, so this produces the same `lazyagent` command as the packaged installs — keep only one of them on your `PATH`. It doesn't include the Wails-powered desktop app because the GUI requires a Node.js build step. Use a desktop package or a source build if you want the GUI.

## Build from source

```bash
git clone https://github.com/illegalstudio/lazyagent
cd lazyagent

# TUI only — no Wails, no Node.js required
make tui

# Full build with desktop support (requires Node.js 18+ and platform libraries)
make install   # npm install, only the first time
make build

# Linux release packages (requires Wails 3, GTK3, and WebKitGTK 4.1)
make linux-packages VERSION=0.13.6
```

The resulting binary is written to the repository root as `lazyagent` — same naming convention as `go install`, unrelated to the retired `lazyagent` command name.

## Verify

```bash
lazyagent --version   # whichever install owns the command: cask link, formula, or go install
```

lazyagent reads its configuration from `~/.config/lazyagent/config.json`, creating it with defaults on first run. See [Configuration](../reference/configuration.md) for the field-by-field reference.
