package discovery

import (
	"context"
	"crypto/rand"
	"time"

	"github.com/skoveit/skovenet/pkg/logger"
	"github.com/skoveit/skovenet/pkg/node"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

const ServiceTag = "mesh-c2"

type MDNSDiscovery struct {
	node    *node.Node
	service mdns.Service
}

func NewMDNSDiscovery(n *node.Node) *MDNSDiscovery {
	return &MDNSDiscovery{node: n}
}

func (d *MDNSDiscovery) Start() error {
	s := mdns.NewMdnsService(d.node.Host(), ServiceTag, d)
	if err := s.Start(); err != nil {
		return err
	}
	d.service = s
	logger.Debugln("mDNS discovery started")
	return nil
}

func (d *MDNSDiscovery) Stop() error {
	if d.service != nil {
		return d.service.Close()
	}
	return nil
}

func (d *MDNSDiscovery) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == d.node.ID() {
		return
	}

	// Skip if already connected
	if d.node.PeerManager().Has(pi.ID) {
		return
	}

	// Connection Manager handles limits - just add to peerstore
	// GossipSub will decide which peers to actually mesh with
	d.node.Host().Peerstore().AddAddrs(pi.ID, pi.Addrs, peerstore.PermanentAddrTTL)

	// Random jitter for OPSEC
	jitter := time.Duration(randInt(500, 2000)) * time.Millisecond
	time.Sleep(jitter)

	d.connectPeer(pi)
}

func (d *MDNSDiscovery) connectPeer(pi peer.AddrInfo) {
	ctx, cancel := context.WithTimeout(d.node.Context(), 10*time.Second)
	defer cancel()

	if err := d.node.Host().Connect(ctx, pi); err != nil {
		return
	}

	d.node.PeerManager().Add(pi.ID)
}

func randInt(min, max int) int {
	b := make([]byte, 4)
	rand.Read(b)
	n := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if n < 0 {
		n = -n
	}
	return min + (n % (max - min + 1))
}
