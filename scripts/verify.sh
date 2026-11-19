#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

mode="${1:-full}"
case "$mode" in
  fast|full) ;;
  *) printf 'Usage: %s [fast|full]\n' "$0" >&2; exit 2 ;;
esac

report_root="${VERIFY_REPORT_DIR:-$repo_root/verification-results}"
mkdir -p "$report_root/$mode"
report_dir="$(cd "$report_root/$mode" && pwd)"
build_dir="$(mktemp -d "${TMPDIR:-/tmp}/skovenet-verify.XXXXXX")"

finish() {
  result=$?
  trap - EXIT
  rm -rf -- "$build_dir"
  if [[ "$result" -eq 0 ]]; then
    printf 'passed\n' > "$report_dir/status.txt"
    printf '\nVerification passed. Reports: %s\n' "$report_dir"
  else
    printf 'failed\n' > "$report_dir/status.txt"
    printf '\nVerification failed. Reports: %s\n' "$report_dir" >&2
  fi
  exit "$result"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

printf 'running\n' > "$report_dir/status.txt"
for report in vet.log tests.log coverage.out coverage.log build.log \
  agent-help.log controller-help.log generator-build.log generator-vet.log \
  generator-tests.log generator-version.log generator-platforms.log; do
  rm -f -- "$report_dir/$report"
done
printf '# SkoveNet verification (%s)\n\n' "$mode" > "$report_dir/summary.md"
printf 'Toolchain: `%s`\n\n' "$(go version)" >> "$report_dir/summary.md"

check() {
  name="$1"
  shift
  printf '[verify] %s\n' "$name"
  if "$@" > "$report_dir/$name.log" 2>&1; then
    printf -- '- PASS: %s\n' "$name" >> "$report_dir/summary.md"
  else
    result=$?
    printf -- '- FAIL: %s (exit %s)\n' "$name" "$result" >> "$report_dir/summary.md"
    cat "$report_dir/$name.log" >&2
    exit "$result"
  fi
}

check vet go vet ./pkg/... ./agent ./controller

if [[ "$mode" == fast ]]; then
  check tests go test -json -count=1 -race -short -timeout=180s \
    -coverprofile="$report_dir/coverage.out" ./pkg/... ./agent ./controller
else
  check tests go test -json -count=1 -race -tags=integration -timeout=180s \
    -coverprofile="$report_dir/coverage.out" ./pkg/... ./agent ./controller
fi

check coverage go tool cover -func="$report_dir/coverage.out"
check build env CGO_ENABLED=0 go build -trimpath -o "$build_dir/" ./agent ./controller
exe="$(go env GOEXE)"
check agent-help "$build_dir/agent$exe" -h
check controller-help "$build_dir/controller$exe" -h

if [[ "$mode" == full ]]; then
  check generator-build make sgen
  check generator-vet go vet ./sgen
  check generator-tests go test -json -count=1 -race -timeout=180s ./sgen
  host_os="$(go env GOOS)"
  check generator-version "$repo_root/bin/$host_os/sgen$exe" version
  check generator-platforms "$repo_root/bin/$host_os/sgen$exe" list
fi
