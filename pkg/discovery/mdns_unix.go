//go:build !windows

package discovery

// SuppressMDNSWarnings is a no-op on non-Windows platforms.
func SuppressMDNSWarnings() {
	// No action needed on Unix systems
}
