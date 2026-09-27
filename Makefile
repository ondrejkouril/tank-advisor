# wotctx build targets.
#
# `just` is not installed on the development machine but GNU make is, so make it is.
# Recipes assume a POSIX shell (Git Bash on Windows).

GO      ?= go
EXE     := $(shell $(GO) env GOEXE)
BIN     := wotctx$(EXE)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# WG_APP_ID is the project's standalone application id. Release builds pass it from a CI
# secret (docs/spec-desktop.md section 12.1); local builds leave it empty and use
# the one stored with `wotctx auth wg --application-id`.
WG_APP_ID ?=
LDFLAGS := -s -w -X main.version=$(VERSION) -X github.com/ondrejkouril/tank-advisor/internal/wg.builtinApplicationID=$(WG_APP_ID)

SKILL_SRC  := skills/wot-advisor
SKILL_DEST ?= $(HOME)/.claude/skills/wot-advisor

# Cross-compilation targets. Both must build from one tree (spec section 11).
PLATFORMS := windows/amd64 darwin/arm64 linux/amd64

.PHONY: all build install test lint fmt tidy release install-skill bundle app installer plugin-validate mod mod-test mod-install clean

all: lint test build

build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/wotctx

# Installs the binary into GOBIN (default ~/go/bin), so the skill can run `wotctx`
# from any directory.
install:
	$(GO) install -trimpath -ldflags '$(LDFLAGS)' ./cmd/wotctx
	@echo "installed $$($(GO) env GOPATH)/bin/$(BIN)"

test:
	$(GO) test ./...

# No golangci-lint dependency: vet plus a gofmt diff catches enough.
lint:
	$(GO) vet ./...
	@unformatted=$$($(GO) fmt -l ./... 2>/dev/null || gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

fmt:
	$(GO) fmt ./...

tidy:
	$(GO) mod tidy

release:
	@rm -rf dist && mkdir -p dist
	@for p in $(PLATFORMS); do \
		os=$${p%%/*}; arch=$${p##*/}; \
		ext=''; [ "$$os" = windows ] && ext='.exe'; \
		echo "building dist/wotctx-$$os-$$arch$$ext"; \
		GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/wotctx-$$os-$$arch$$ext ./cmd/wotctx || exit 1; \
	done

# Installs the reasoning layer. Source of truth stays in this repo; this copies it
# to the personal skills directory so it is available in every project.
install-skill:
	@if [ ! -f "$(SKILL_SRC)/SKILL.md" ]; then \
		echo "$(SKILL_SRC)/SKILL.md does not exist yet (plan step 11)"; exit 1; \
	fi
	mkdir -p "$(SKILL_DEST)"
	cp -R "$(SKILL_SRC)/." "$(SKILL_DEST)/"
	@echo "installed to $(SKILL_DEST)"

# The Claude Desktop bundle (docs/plan.md, steps P4 and D4): the launcher, which
# runs the wotctx the Tank Advisor app installed, plus packaging/mcpb/manifest.json,
# packed by the official mcpb tool (needs Node). The bundle carries no wotctx of
# its own, so it needs reinstalling only when the manifest changes.
BUNDLE_VERSION ?= 0.5.0
MCPB           ?= npx -y @anthropic-ai/mcpb

bundle:
	@rm -rf dist/mcpb && mkdir -p dist/mcpb/server
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags '-s -w -X main.version=$(VERSION)' -o dist/mcpb/server/wotctx-launcher.exe ./cmd/wotctx-launcher
	sed 's/@VERSION@/$(BUNDLE_VERSION)/' packaging/mcpb/manifest.json > dist/mcpb/manifest.json
	$(MCPB) validate dist/mcpb/manifest.json
	$(MCPB) pack dist/mcpb dist/wotctx-$(BUNDLE_VERSION).mcpb

# The Tank Advisor app (docs/plan.md, step D5), into dist/app/ with what ships
# beside it: the newest client mod package and Desktop bundle found in dist/,
# or the ones named by MOD_PKG and BUNDLE. Windows only; a GUI executable, so
# it opens no console.
APP_DIR ?= dist/app
MOD_PKG ?= $(shell ls -t dist/ondrejkouril.wotctx_*.wotmod 2>/dev/null | head -1)
BUNDLE  ?= $(shell ls -t dist/wotctx-*.mcpb 2>/dev/null | head -1)

app:
	mkdir -p $(APP_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS) -H windowsgui' -o $(APP_DIR)/TankAdvisor.exe ./cmd/tankadvisor
	@if [ -n "$(MOD_PKG)" ]; then cp "$(MOD_PKG)" $(APP_DIR)/; else echo "no mod package in dist/ (make mod, or pass MOD_PKG=)"; fi
	@if [ -n "$(BUNDLE)" ]; then cp "$(BUNDLE)" $(APP_DIR)/; else echo "no Desktop bundle in dist/ (make bundle, or pass BUNDLE=)"; fi

# The installer (docs/plan.md, step D9): wotctx, the Desktop bundle and the mod
# go into cmd/tankadvisor/payload/, the app is built carrying them, and NSIS
# wraps it. A release passes VERSION (v1.2.3 or v1.2.3-rc.1) and WG_APP_ID.
# Needs NSIS (MAKENSIS), Node for the bundle, and Microsoft's WebView2
# bootstrapper (downloaded once into dist/).
MAKENSIS    ?= makensis
NSIS_FLAGS  ?=
WEBVIEW2    ?= dist/MicrosoftEdgeWebview2Setup.exe
VERSION_NUM  = $(or $(shell echo '$(VERSION)' | sed -nE 's/^v?([0-9]+[.][0-9]+[.][0-9]+).*/\1/p'),0.0.0)
PAYLOAD     := cmd/tankadvisor/payload
# NSIS reads only Windows paths.
winpath = $(subst /,\,$(abspath $(1)))

installer: bundle
	@if [ -z "$(MOD_PKG)" ]; then echo "no mod package: make mod, or pass MOD_PKG="; exit 1; fi
	find $(PAYLOAD) -type f ! -name README.md -delete
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(PAYLOAD)/wotctx.exe ./cmd/wotctx
	cp dist/wotctx-$(BUNDLE_VERSION).mcpb "$(MOD_PKG)" $(PAYLOAD)/
	mkdir -p dist/installer
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags '$(LDFLAGS) -H windowsgui' -o dist/installer/TankAdvisor.exe ./cmd/tankadvisor
	find $(PAYLOAD) -type f ! -name README.md -delete
	@if [ ! -f "$(WEBVIEW2)" ]; then curl -fsSL -o "$(WEBVIEW2)" "https://go.microsoft.com/fwlink/p/?LinkId=2124703"; fi
	cd packaging/nsis && "$(MAKENSIS)" -V2 $(NSIS_FLAGS) -DVERSION='$(VERSION)' -DVERSION_NUM='$(VERSION_NUM)' \
		-DAPP="$(call winpath,dist/installer/TankAdvisor.exe)" -DWEBVIEW2="$(call winpath,$(WEBVIEW2))" \
		-DOUTFILE="$(call winpath,dist/installer/TankAdvisor-$(VERSION)-setup.exe)" tankadvisor.nsi
	@echo "built dist/installer/TankAdvisor-$(VERSION)-setup.exe"

# Validates .claude-plugin/ (the plugin and its marketplace). Needs Claude Code.
plugin-validate:
	claude plugin validate .claude-plugin/plugin.json
	claude plugin validate .

# The World of Tanks client mod (docs/plan.md, phase 4). It needs Python 2.7,
# the client's version, to compile and to run its tests.
PY27 ?= C:/Python27/python.exe

mod-test:
	cd mod && $(PY27) -B -m unittest discover -s . -p 'test_*.py'

mod: mod-test
	$(PY27) -B mod/build.py dist

# Copies the newest built package into the game's mods/<version>/ folder.
mod-install: mod build
	./$(BIN) mod install "$$(ls -t dist/ondrejkouril.wotctx_*.wotmod | head -1)"

clean:
	rm -rf dist $(BIN)
