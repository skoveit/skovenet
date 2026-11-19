#!/usr/bin/env bash
# Verify the sgen agent generator end to end.
#
# Usage: scripts/verify-generator.sh [native|all|os/arch[,os/arch...]]
#   Target defaults to $SGEN_SMOKE_TARGETS, then "native".
#
# Steps:
#   1. Rebuild sgen with a freshly generated embedded source archive
#      (make sgen; downloads the host Go toolchain archive if missing).
#   2. Run the sgen unit tests (go test -race ./sgen).
#   3. Run the generator_integration tests: the embedded source must match
#      the checkout, then Build generates an agent for each target with the
#      compiler cache, toolchain and outputs under a test temp directory.
#      Binaries are checked for format/architecture and the injected key;
#      only the native binary is executed, and only with -h.
#
# Environment: SGEN_SMOKE_TIMEOUT (go test -timeout, default 60m).
set -euo pipefail

cd "$(dirname "$0")/.."

targets="${1:-${SGEN_SMOKE_TARGETS:-native}}"

echo "[*] Regenerating embedded agent source and building sgen"
rm -f sgen/assets/source.tar.gz
make sgen

echo "[*] sgen unit tests"
go test -count=1 -race ./sgen

echo "[*] Generated-agent build smoke: ${targets}"
SGEN_SMOKE_TARGETS="${targets}" go test -tags generator_integration -count=1 \
	-timeout "${SGEN_SMOKE_TIMEOUT:-60m}" -v \
	-run '^(TestEmbeddedSourceMatchesTree|TestGeneratedAgentBuild)$' ./sgen

echo "[✓] Generator verification passed (${targets})"
