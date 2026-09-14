.PHONY: all build tui frontend bindings clean dev install deploy app \
	linux-desktop linux-deb linux-rpm linux-arch linux-appimage linux-packages

VERSION ?= 0.0.0
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GOARCH ?= $(shell go env GOARCH)
LINUX_BINARY := dist/linux-desktop/lazyagent
LINUX_LDFLAGS := -s -w \
	-X github.com/illegalstudio/lazyagent/internal/version.Version=$(VERSION) \
	-X github.com/illegalstudio/lazyagent/internal/version.Commit=$(COMMIT)
# GOARCH under its kernel name. AppImage and pacman both label artifacts
# this way, and the pacman repository routes a package by that label.
LINUX_ARCH := $(if $(filter amd64,$(GOARCH)),x86_64,aarch64)

all: build

# Build with frontend (TUI + GUI support)
build: frontend
	rm -rf internal/assets/dist/*
	cp -r frontend/dist/. internal/assets/dist/
	go build -tags production -o lazyagent .

# Build TUI only (no frontend or Wails needed)
tui:
	go build -tags notray -o lazyagent .

# Build the frontend
frontend: bindings
	cd frontend && npm run build

# Generate Wails bindings
bindings:
	wails3 generate bindings -d frontend/src/bindings -ts .

# Install frontend dependencies
install:
	cd frontend && npm install

# Dev mode: rebuild frontend and run GUI app
dev: bindings
	cd frontend && npm run build
	rm -rf internal/assets/dist/*
	cp -r frontend/dist/. internal/assets/dist/
	go run . --gui

# Interactive release: propose next semver tag, then tag + push to origin.
deploy:
	@scripts/deploy.sh

# Assemble an unsigned Lazyagent.app locally (for testing the bundle)
app: build
	scripts/make-app.sh ./lazyagent dev dist-app

# Full Linux desktop build (GUI + TUI + API). GTK3 is explicit so a future
# Wails upgrade cannot silently change the runtime ABI used by release assets.
linux-desktop: frontend
	rm -rf internal/assets/dist/*
	cp -r frontend/dist/. internal/assets/dist/
	mkdir -p dist/linux-desktop
	CGO_ENABLED=1 GOOS=linux GOARCH=$(GOARCH) go build \
		-tags production,gtk3 -trimpath -ldflags "$(LINUX_LDFLAGS)" \
		-o $(LINUX_BINARY) .

linux-deb: linux-desktop
	VERSION=$(VERSION) GOARCH=$(GOARCH) wails3 tool package \
		-name "Lazyagent_$(VERSION)_linux_$(GOARCH)" -format deb \
		-config build/linux/nfpm.yaml -out dist

linux-rpm: linux-desktop
	VERSION=$(VERSION) GOARCH=$(GOARCH) wails3 tool package \
		-name "Lazyagent_$(VERSION)_linux_$(GOARCH)" -format rpm \
		-config build/linux/nfpm.yaml -out dist

# Arch's canonical file name (pkgname-pkgver-pkgrel-arch), unlike the deb and
# rpm above: illegalstudio/pacman picks the architecture directory from the
# last dash-separated field of the asset name, so a Lazyagent_..._amd64 name
# would make the repository skip the package.
linux-arch: linux-desktop
	VERSION=$(VERSION) GOARCH=$(GOARCH) wails3 tool package \
		-name "lazyagent-$(VERSION)-1-$(LINUX_ARCH)" -format archlinux \
		-config build/linux/nfpm.yaml -out dist

linux-appimage: linux-desktop
	rm -rf dist/appimage-build
	cp assets/appicon.png dist/linux-desktop/lazyagent.png
	# Keep linuxdeploy's output name aligned with the filename Wails expects
	# to move out of the build directory after packaging.
	cd dist/linux-desktop && LDAI_OUTPUT="lazyagent-$(LINUX_ARCH).AppImage" \
		wails3 generate appimage \
		-binary lazyagent \
		-icon "$(abspath dist/linux-desktop/lazyagent.png)" \
		-desktopfile "$(abspath build/linux/lazyagent.desktop)" \
		-outputdir "$(abspath dist)" \
		-builddir "$(abspath dist/appimage-build)"
	mv "dist/lazyagent-$(LINUX_ARCH).AppImage" \
		"dist/Lazyagent_$(VERSION)_linux_$(GOARCH).AppImage"

linux-packages: linux-deb linux-rpm linux-arch linux-appimage

# Clean build artifacts
clean:
	rm -f lazyagent
	rm -rf frontend/dist internal/assets/dist/*
	rm -rf dist-app dist
