# Contributing to SkoveNet


### General Principles
- **Minimal Dependencies**: We prefer the Go standard library. Only add external dependencies if absolutely necessary.
- **Thread Safety**: SkoveNet is highly concurrent. Use mutexes (`sync.Mutex`, `sync.RWMutex`) and atomic operations to ensure thread safety.
- **Idiomatic Go**: Follow [Effective Go](https://golang.org/doc/effective_go.html) and standard `gofmt` formatting.
- Always run tests before submitting a Pull Request.

Run `make verify-fast` while developing and `make verify` before opening a PR.
These commands stop on a failed check and write test logs, coverage, and a summary
to `verification-results/`. CI runs the fast checks on Linux, macOS, and Windows,
plus the full integration and generator checks on Linux. See [VERIFYING.md](VERIFYING.md)
for the check scope and prerequisites.

## PR

1. **Branching**: Create a feature branch from `main`.
2. **Commit Messages**: Use descriptive, imperative-style commit messages.
3. **Draft PR**: Feel free to open a Draft PR early to get feedback.
4. **Review**: All PRs require review from a maintainer. Ensure CI passes.

## Legal & Security

### License
By contributing to SkoveNet, you agree that your contributions will be licensed under the **GPLv3 License**.

### Security
If you discover a security vulnerability, please refer to our [SECURITY.md](SECURITY.md) for reporting instructions.

### Legal Disclaimer
SkoveNet is intended for authorized security research and penetration testing only. Unauthorized use against systems you don't own is illegal. Contributors are expected to act ethically and within legal boundaries.
