package node

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// ---------------------------------------------------------------------------
// Basic add / remove / count
// ---------------------------------------------------------------------------

func TestPeerManager_AddAndCount(t *testing.T) {
	pm := NewPeerManager(5)

	if pm.Count() != 0 {
		t.Fatalf("new PeerManager should have 0 peers, got %d", pm.Count())
	}

	added := pm.Add(peer.ID("peer-1"))
	if !added {
		t.Error("Add should return true for a new peer")
	}
	if pm.Count() != 1 {
		t.Errorf("Count = %d, want 1", pm.Count())
	}
}

func TestPeerManager_AddDuplicate(t *testing.T) {
	pm := NewPeerManager(5)
	pm.Add(peer.ID("peer-1"))
	added := pm.Add(peer.ID("peer-1"))

	if added {
		t.Error("Add should return false for a duplicate peer")
	}
	if pm.Count() != 1 {
		t.Errorf("Count = %d, want 1 (no duplicate)", pm.Count())
	}
}

func TestPeerManager_Remove(t *testing.T) {
	pm := NewPeerManager(5)
	pm.Add(peer.ID("peer-1"))
	pm.Remove(peer.ID("peer-1"))

	if pm.Count() != 0 {
		t.Errorf("Count = %d after remove, want 0", pm.Count())
	}
}

func TestPeerManager_RemoveNonexistent(t *testing.T) {
	pm := NewPeerManager(5)
	// Should not panic
	pm.Remove(peer.ID("ghost"))
	if pm.Count() != 0 {
		t.Error("removing nonexistent peer should not change count")
	}
}

// ---------------------------------------------------------------------------
// Max peers / IsFull
// ---------------------------------------------------------------------------

func TestPeerManager_MaxPeers(t *testing.T) {
	pm := NewPeerManager(3)

	pm.Add(peer.ID("a"))
	pm.Add(peer.ID("b"))
	pm.Add(peer.ID("c"))

	if !pm.IsFull() {
		t.Error("should be full at 3/3")
	}

	added := pm.Add(peer.ID("d"))
	if added {
		t.Error("Add should return false when full")
	}
	if pm.Count() != 3 {
		t.Errorf("Count = %d, want 3 (should not exceed max)", pm.Count())
	}
}

func TestPeerManager_AddAfterRemove(t *testing.T) {
	pm := NewPeerManager(2)
	pm.Add(peer.ID("a"))
	pm.Add(peer.ID("b"))

	// Full — remove one, then add should succeed
	pm.Remove(peer.ID("a"))
	added := pm.Add(peer.ID("c"))
	if !added {
		t.Error("Add should succeed after removing a peer from a full manager")
	}
}

// ---------------------------------------------------------------------------
// Has
// ---------------------------------------------------------------------------

func TestPeerManager_Has(t *testing.T) {
	pm := NewPeerManager(5)
	pm.Add(peer.ID("peer-1"))

	if !pm.Has(peer.ID("peer-1")) {
		t.Error("Has should return true for added peer")
	}
	if pm.Has(peer.ID("peer-2")) {
		t.Error("Has should return false for unknown peer")
	}
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func TestPeerManager_List(t *testing.T) {
	pm := NewPeerManager(5)
	pm.Add(peer.ID("a"))
	pm.Add(peer.ID("b"))
	pm.Add(peer.ID("c"))

	list := pm.List()
	if len(list) != 3 {
		t.Fatalf("List length = %d, want 3", len(list))
	}

	// All peers should be present
	found := make(map[peer.ID]bool)
	for _, p := range list {
		found[p] = true
	}
	for _, id := range []peer.ID{"a", "b", "c"} {
		if !found[id] {
			t.Errorf("peer %q not found in List()", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Callback
// ---------------------------------------------------------------------------

func TestPeerManager_Callback(t *testing.T) {
	pm := NewPeerManager(5)

	type event struct {
		id        string
		connected bool
	}
	ch := make(chan event, 10)

	pm.SetCallback(func(peerID string, connected bool) {
		ch <- event{peerID, connected}
	})

	pid := peer.ID("peer-1")
	expectedID := pid.String()

	pm.Add(pid)
	pm.Remove(pid)

	// Collect both events (order may vary due to goroutine scheduling)
	var gotConnect, gotDisconnect bool
	for i := 0; i < 2; i++ {
		select {
		case e := <-ch:
			if e.id != expectedID {
				t.Errorf("unexpected peer ID: got %q, want %q", e.id, expectedID)
			}
			if e.connected {
				gotConnect = true
			} else {
				gotDisconnect = true
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for callback")
		}
	}

	if !gotConnect {
		t.Error("missing connect callback")
	}
	if !gotDisconnect {
		t.Error("missing disconnect callback")
	}
}
