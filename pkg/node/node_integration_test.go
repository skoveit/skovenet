//go:build integration

package node

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
)

func TestNodeIntegration_SuppliedHostAndDisconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hosts := make([]*Node, 2)
	for i := range hosts {
		h, err := libp2p.New(
			libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
			libp2p.DisableRelay(),
		)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { h.Close() })
		n, err := NewNodeWithHost(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { n.PubSub().Close() })
		if n.Host() != h || n.ID() != h.ID() || n.Context() != ctx || n.PubSub() == nil {
			t.Fatal("node did not retain the supplied host and initialize mesh services")
		}
		if n.PeerManager().Count() != 0 || n.ListPeers() != "No connected peers" {
			t.Fatal("new node unexpectedly has tracked peers")
		}
		hosts[i] = n
	}
	a, b := hosts[0], hosts[1]
	if err := a.Host().Connect(ctx, peer.AddrInfo{ID: b.ID(), Addrs: b.Addrs()}); err != nil {
		t.Fatalf("connect loopback hosts: %v", err)
	}
	a.PeerManager().Add(b.ID())
	disconnected := make(chan string, 1)
	a.PeerManager().SetCallback(func(id string, connected bool) {
		if !connected {
			disconnected <- id
		}
	})
	if err := a.Host().Network().ClosePeer(b.ID()); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-disconnected:
		if id != b.ID().String() || a.PeerManager().Has(b.ID()) {
			t.Fatal("disconnect notification did not remove the correct peer")
		}
	case <-ctx.Done():
		t.Fatal("node did not remove a disconnected peer")
	}
}
