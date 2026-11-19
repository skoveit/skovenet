package node

import (
	"context"
	"testing"
)

func TestNewNodeWithHost_RejectsNilHost(t *testing.T) {
	n, err := NewNodeWithHost(context.Background(), nil)
	if err == nil || n != nil {
		t.Fatalf("NewNodeWithHost(nil) = (%v, %v), want nil node and error", n, err)
	}
}
