# Site Sentinel ships source only: bin/ is not committed, so the plugin is built
# once after install. Requires the Go toolchain.

VERSION ?= 0.1.2
LDFLAGS := -X main.version=$(VERSION)

# Every tool is named through a variable, and the two that only ever do one
# thing are named absolutely, so a build does not depend on what PATH resolves
# them to. A packager can override any of them.
GO      ?= go
INSTALL ?= /usr/bin/install

# Pin the compiler. Without this the go command will fetch a different
# toolchain over the network to satisfy the directive in go.mod; with it the
# build uses the toolchain that is installed, or fails and says so.
export GOTOOLCHAIN = local

# Go turns cgo on whenever it finds a C compiler and off when it does not, so
# the same source silently produces different binaries on different machines,
# and picks a different DNS resolver with it. Pin it off: no C toolchain is
# needed, and the pure Go resolver is the one that reads resolv.conf directly.
export CGO_ENABLED = 0

# readonly refuses to edit go.mod or go.sum during a build, so every module is
# the one the committed checksums name. trimpath keeps the output free of local
# paths, and buildvcs=false keeps the git revision out, so the bytes depend on
# the source and the toolchain and nothing else.
BUILDFLAGS := -trimpath -mod=readonly -buildvcs=false

PLUGIN_DIR ?= $(HOME)/.config/omarchy/plugins/karamble.sitesentinel
PLUGIN_FILES := manifest.json Panel.qml Service.qml DashboardView.qml SitesView.qml \
		AlertsView.qml SiteForm.qml ArmForm.qml SettingsView.qml ListRow.qml \
		Badge.qml README.md LICENSE
# preview.png is generated, so it is copied only when it exists.
PREVIEW := $(wildcard preview.png)

.PHONY: all build test verify toolchain clean install install-check lint

all: build

# The FAQ the toolchain preflight points at. Install instructions belong in
# documentation a person can read and correct, not in a build target.
FAQ_URL := https://github.com/karamble/omarchy-site-sentinel/blob/master/docs/FAQ.md

# Building from source needs Go, and the most common way to miss it on Omarchy
# is having it under mise without the shell activated. Say which case it is.
toolchain:
	@command -v $(GO) >/dev/null 2>&1 && exit 0; \
	echo "Site Sentinel builds from source and the Go toolchain is not on PATH."; \
	echo; \
	if command -v mise >/dev/null 2>&1 && mise which go >/dev/null 2>&1; then \
		echo "  mise has Go, but this shell cannot see it. Open a new terminal and"; \
		echo "  press Build again, or build against it directly:"; \
		echo; \
		echo "      make GO=$$(mise which go)"; \
	elif command -v mise >/dev/null 2>&1; then \
		echo "  Omarchy ships mise, so the shortest way is:"; \
		echo; \
		echo "      mise use -g go@latest"; \
		echo; \
		echo "  then open a new terminal and press Build again."; \
	else \
		echo "  Installing Go, or keeping toolchains in your home directory:"; \
		echo; \
		echo "      $(FAQ_URL)"; \
	fi; \
	echo; \
	echo "Go $(shell sed -n 's/^go \([0-9.]*\)$$/\1/p' go.mod) or newer is needed."; \
	exit 1

build: toolchain verify
	@$(INSTALL) -d bin
	@echo "building bin/sentinel"
	@$(GO) build $(BUILDFLAGS) -ldflags "$(LDFLAGS)" -o bin/sentinel ./cmd/sentinel
	@echo
	@echo "built. next:"
	@echo "  ./bin/sentinel add https://example.com"
	@echo "  ./bin/sentinel status"

# CGO_ENABLED is pinned off above so the shipped binary is reproducible, but the
# race detector is built on cgo and refuses to run without it. Turning it back
# on for this one target keeps both: a reproducible build and a suite that can
# actually be raced. Without the override this target only ever printed
# "-race requires cgo".
test:
	CGO_ENABLED=1 $(GO) test -race ./...

# Omarchy refuses symlinks inside a plugin folder, so installing copies.
install: build
	@$(INSTALL) -d "$(PLUGIN_DIR)/bin"
	@$(INSTALL) -m 0644 $(PLUGIN_FILES) $(PREVIEW) "$(PLUGIN_DIR)/"
	@# install writes through a fresh inode, so a running daemon holding the old
	@# binary open does not block the replacement, which plain cp would.
	@$(INSTALL) -m 0755 bin/sentinel "$(PLUGIN_DIR)/bin/"
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

# The qmllint and qmlformat on PATH may not be Qt 6's: some distributions ship
# an unrelated binary of the same name that reports version 1.0 and fails on
# `pragma ComponentBehavior: Bound` with no output at all. Prefer Qt's own.
QMLLINT   := $(shell command -v qmllint6 2>/dev/null || echo /usr/lib/qt6/bin/qmllint)
QMLFORMAT := $(shell command -v qmlformat6 2>/dev/null || echo /usr/lib/qt6/bin/qmlformat)
SHELL_DIR := $(or $(OMARCHY_PATH),/usr/share/omarchy)/shell
LINTROOT  := $(CURDIR)/.lintroot
QMLFILES  := $(shell find . -name '*.qml' -not -path './.git/*' -not -path './.lintroot/*')

lint:
	@for f in $(QMLFILES); do $(QMLFORMAT) "$$f" >/dev/null || { echo "failed to parse $$f"; exit 1; }; done
	@echo "qml: all files parse"
	@# `import qs.Ui` resolves as <import path>/qs/Ui/qmldir, so the shell has
	@# to be reachable under a directory named `qs`.
	@mkdir -p $(LINTROOT) && ln -sfn $(SHELL_DIR) $(LINTROOT)/qs
	$(QMLLINT) -I $(LINTROOT) $(QMLFILES)
	@rm -rf $(LINTROOT)
