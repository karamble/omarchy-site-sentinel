# Site Sentinel ships source only: bin/ is not committed, so the plugin is built
# once after install. Requires the Go toolchain.

VERSION ?= 0.1.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT)

PLUGIN_DIR ?= $(HOME)/.config/omarchy/plugins/karamble.sitesentinel
PLUGIN_FILES := manifest.json Panel.qml Service.qml DashboardView.qml SitesView.qml \
		AlertsView.qml SiteForm.qml ArmForm.qml SettingsView.qml ListRow.qml \
		Badge.qml README.md LICENSE
# preview.png is generated, so it is copied only when it exists.
PREVIEW := $(wildcard preview.png)

.PHONY: all build test clean install install-check

all: build

build:
	@mkdir -p bin
	@echo "building bin/sentinel"
	@go build -ldflags "$(LDFLAGS)" -o bin/sentinel ./cmd/sentinel
	@echo
	@echo "built. next:"
	@echo "  ./bin/sentinel add https://example.com"
	@echo "  ./bin/sentinel status"

test:
	go test -race ./...

# Omarchy refuses symlinks inside a plugin folder, so installing copies.
install: build
	@mkdir -p "$(PLUGIN_DIR)/bin"
	@cp --remove-destination $(PLUGIN_FILES) $(PREVIEW) "$(PLUGIN_DIR)/"
	@# --remove-destination unlinks first, so a running daemon does not block the copy
	@cp --remove-destination bin/sentinel "$(PLUGIN_DIR)/bin/"
	@echo "installed to $(PLUGIN_DIR)"
	@echo "enable it with: omarchy plugin enable karamble.sitesentinel right"

clean:
	rm -rf bin

install-check:
	@command -v go >/dev/null || { echo "go toolchain not found"; exit 1; }
	@echo "toolchain ok"
