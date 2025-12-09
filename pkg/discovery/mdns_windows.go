//go:build windows

package discovery

import (
	"io"
	"log"
	"os"
	"strings"
)

// mdnsLogFilter filters out noisy mDNS warnings on Windows
type mdnsLogFilter struct {
	original io.Writer
}

func (f *mdnsLogFilter) Write(p []byte) (n int, err error) {
	msg := string(p)
	// Suppress the multicast interface warnings that spam on Windows
	if strings.Contains(msg, "mdns: Failed to set multicast interface") {
		return len(p), nil // Pretend we wrote it
	}
	return f.original.Write(p)
}

// SuppressMDNSWarnings redirects log output to filter mDNS warnings on Windows.
// Call this before starting mDNS discovery.
func SuppressMDNSWarnings() {
	log.SetOutput(&mdnsLogFilter{original: os.Stderr})
}
