# ──────────────────────────────────────────────────────────────
# SkoveNet — Build System
# ──────────────────────────────────────────────────────────────
#
# Usage:
#   make sgen          Build sgen (downloads toolchain + vendors deps)
#   make controller    Build the controller
#   make clean         Remove build artifacts
#
# Requirements:
#   - Go toolchain (for building sgen itself)
#   - curl (for downloading the Go toolchain to embed)
#   - tar  (for creating source archives)
# ──────────────────────────────────────────────────────────────

GO_VERSION   := 1.25.4
# Target OS/Arch for the embedded toolchain and the sgen binary itself.
# These can be overridden: make sgen HOST_OS=windows HOST_ARCH=amd64
HOST_OS      ?= $(shell go env GOOS)
HOST_ARCH    ?= $(shell go env GOARCH)
GO_DL_EXT    := $(if $(filter windows,$(HOST_OS)),zip,tar.gz)
GO_DL_URL    := https://go.dev/dl/go$(GO_VERSION).$(HOST_OS)-$(HOST_ARCH).$(GO_DL_EXT)

SGEN_ASSETS  := sgen/assets
BIN_DIR      := bin

.PHONY: sgen controller clean help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

# ──────────────────────────────────────────────────────────────
# sgen — Agent Generator
# ──────────────────────────────────────────────────────────────

# Determine binary name and path
SGEN_BIN_NAME := sgen$(if $(filter windows,$(HOST_OS)),.exe,)
SGEN_BIN_PATH := $(BIN_DIR)/$(HOST_OS)/$(SGEN_BIN_NAME)

CTRL_BIN_NAME := controller$(if $(filter windows,$(HOST_OS)),.exe,)
CTRL_BIN_PATH := $(BIN_DIR)/$(HOST_OS)/$(CTRL_BIN_NAME)

sgen: $(SGEN_ASSETS)/toolchain.$(GO_DL_EXT) $(SGEN_ASSETS)/source.tar.gz ## Build sgen with embedded toolchain + source
	@mkdir -p $(BIN_DIR)/$(HOST_OS)
	@echo "[*] Building sgen for $(HOST_OS)/$(HOST_ARCH)..."
	CGO_ENABLED=0 GOOS=$(HOST_OS) GOARCH=$(HOST_ARCH) go build -ldflags="-s -w" -o $(SGEN_BIN_PATH) ./sgen
	@echo "[✓] sgen built: $(SGEN_BIN_PATH)"

# Download the Go toolchain for the host platform.
$(SGEN_ASSETS)/toolchain.$(GO_DL_EXT):
	@mkdir -p $(SGEN_ASSETS)
	@echo "[*] Downloading Go $(GO_VERSION) for $(HOST_OS)/$(HOST_ARCH)..."
	curl -fSL -o $(SGEN_ASSETS)/toolchain.$(GO_DL_EXT) "$(GO_DL_URL)"
	@echo "[✓] Toolchain downloaded"

# Create a source archive with everything needed to build the agent.
$(SGEN_ASSETS)/source.tar.gz: vendor
	@mkdir -p $(SGEN_ASSETS)
	@echo "[*] Creating agent source archive..."
	tar czf $(SGEN_ASSETS)/source.tar.gz \
		go.mod go.sum vendor/ agent/ pkg/
	@echo "[✓] Source archive created"

# Vendor dependencies (only runs if vendor/ doesn't exist).
vendor: go.mod go.sum
	@echo "[*] Vendoring dependencies..."
	go mod vendor
	@touch vendor

# ──────────────────────────────────────────────────────────────
# Controller
# ──────────────────────────────────────────────────────────────

controller: ## Build the controller
	@mkdir -p $(BIN_DIR)/$(HOST_OS)
	@echo "[*] Building controller for $(HOST_OS)/$(HOST_ARCH)..."
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(CTRL_BIN_PATH) ./controller
	@echo "[✓] controller built: $(CTRL_BIN_PATH)"

# ──────────────────────────────────────────────────────────────
# Cleanup
# ──────────────────────────────────────────────────────────────

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)
	rm -rf $(SGEN_ASSETS)/toolchain.*
	rm -rf $(SGEN_ASSETS)/source.tar.gz
	rm -rf vendor/
