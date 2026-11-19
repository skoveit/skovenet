# Automated verification

Run `make verify-fast` during development. Run `make verify` before submitting a
change to include integration-tagged tests, the command timeout check, and the
agent generator. CI runs these checks for every pull request, including stacked
PRs, and for pushes to `main` or `master`. It can also be started manually from
the Actions page.

Both commands stop at the first failed check and return a nonzero exit status.
They use temporary directories for binary smoke checks and preserve reports under
`verification-results/fast/` or `verification-results/full/`:

- `status.txt`: running, passed, or failed.
- `summary.md`: the toolchain version and completed check results.
- `tests.log`: Go test JSON events, including failures and skips.
- `coverage.out` and `coverage.log`: package test coverage data and per-function totals.
- Separate logs for vet, builds, and CLI smoke checks.

Set `VERIFY_REPORT_DIR` to choose another report directory. CI uploads the reports
as artifacts even when a check fails. Concurrent local runs should use distinct
report directories.

## Check scope

| Check | Fast | Full |
| --- | --- | --- |
| Vet application and package code | Yes | Yes |
| Package, agent, and controller tests with the race detector | Short tests | All tests, including the `integration` build tag |
| Coverage report | Yes | Yes |
| Build native agent and controller without CGO | Yes | Yes |
| Start both binaries with `-h` | Yes | Yes |
| Build generator with embedded source and toolchain | No | Yes |
| Vet and test generator | No | Yes |
| Run generator version and platform listing | No | Yes |

The CLI smoke checks use help flags and do not start an agent or join a network.
Integration test assertions live alongside the packages they verify. A successful
check establishes only the behavior exercised by those tests; Go reports tests
that are skipped. WAN/NAT behavior, mDNS across physical interfaces, and graphical
browser interactions need their own environment-specific checks.

## Prerequisites

Use the Go version declared in `go.mod`, a C compiler for the race detector, Bash,
and Make. Full verification also needs `curl` and `tar`. `make sgen` downloads an
approximately 60 MB Go toolchain archive on its first run and prepares vendored
dependencies. Subsequent runs reuse the toolchain and refresh the embedded source
archive, including source edits and deletions. The generator unit tests require
these embedded assets, so run them through `make verify` or build `make sgen`
before calling `go test ./sgen` directly.

GitHub Actions additionally runs fast verification on native Windows and macOS
runners, so cross-platform behavior is exercised rather than inferred from a
successful Linux build. Platform-specific tests should state their constraints
with build tags or explicit skips.
