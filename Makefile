# Site Sentinel ships source only: bin/ is not committed, so the plugin is built
# once after install. Requires the Go toolchain.

VERSION ?= 0.1.1
LDFLAGS := -X main.version=$(VERSION)

# Tools are named through variables so a packager can point them elsewhere.
GO ?= go

# Pin the compiler. Without this the go command will fetch a different
# toolchain over the network to satisfy the directive in go.mod; with it the
# build uses the toolchain that is installed, or fails and says so.
export GOTOOLCHAIN = local

# readonly refuses to edit go.mod or go.sum during a build, so every module is
# the one the committed checksums name. trimpath keeps the output free of local
# paths, so the same inputs give the same bytes.
BUILDFLAGS := -trimpath -mod=readonly

PLUGIN_DIR ?= $(HOME)/.config/omarchy/plugins/karamble.sitesentinel
PLUGIN_FILES := manifest.json Panel.qml Service.qml DashboardView.qml SitesView.qml \
		AlertsView.qml SiteForm.qml ArmForm.qml SettingsView.qml ListRow.qml \
		Badge.qml README.md LICENSE
# preview.png is generated, so it is copied only when it exists.
PREVIEW := $(wildcard preview.png)

.PHONY: all build test verify clean install install-check

all: build

build: verify
	@mkdir -p bin
	@echo "building bin/sentinel"
	@$(GO) build $(BUILDFLAGS) -ldflags "$(LDFLAGS)" -o bin/sentinel ./cmd/sentinel
	@echo
	@echo "built. next:"
	@echo "  ./bin/sentinel add https://example.com"
	@echo "  ./bin/sentinel status"

test:
	$(GO) test -race ./...

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

# Check every module against the committed checksums before anything compiles.
verify:
	@$(GO) mod verify >/dev/null || { echo "module verification failed"; exit 1; }

install-check: verify
	@command -v $(GO) >/dev/null || { echo "go toolchain not found"; exit 1; }
	@echo "toolchain ok, modules verified"
