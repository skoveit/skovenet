#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

go_cmd=${SKOVENET_GO:-go}
test_timeout=${SKOVENET_TEST_TIMEOUT:-120s}
fixture_public_key='/rSMnRsuFuRc/hqy/QPe98h++DMjZQnolH1eNAA866Q='

printf '%s\n' 'Verifying protocol and node behavior with normal operator trust...'
"$go_cmd" test -tags=integration -race -count=1 -timeout="$test_timeout" ./pkg/protocol ./pkg/node

printf '%s\n' 'Verifying signed command execution with the integration-only operator fixture...'
"$go_cmd" test -tags=integration -race -count=1 -timeout="$test_timeout" \
  -ldflags "-X github.com/skoveit/skovenet/pkg/signing.publicKeyB64=$fixture_public_key" \
  ./integration/mesh
