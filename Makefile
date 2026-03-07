.PHONY: all tui app frontend bindings clean dev

all: tui app

# Build the TUI binary
tui:
	go build -o lazyagent ./cmd/tui/

# Build the macOS menu bar app
app: frontend
	cp -r frontend/dist internal/assets/dist
	go build -o lazyagent-app ./cmd/app/

# Build the frontend
frontend: bindings
	cd frontend && npm run build

# Generate Wails bindings
bindings:
	wails3 generate bindings -d frontend/src/bindings -ts ./cmd/app

# Install frontend dependencies
install:
	cd frontend && npm install

# Dev mode: rebuild frontend and run app
dev: bindings
	cd frontend && npm run build
	cp -r frontend/dist internal/assets/dist
	go run ./cmd/app/

# Clean build artifacts
clean:
	rm -f lazyagent lazyagent-app
	rm -rf frontend/dist internal/assets/dist
