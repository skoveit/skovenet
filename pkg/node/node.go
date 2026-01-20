package node

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"

	"github.com/skoveit/skovenet/pkg/logger"
	"github.com/skoveit/skovenet/pkg/pubsub"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
)

// Connection limits matching GossipSub mesh parameters
const (
	// LowWater is the minimum number of connections to maintain (D_lo)
	LowWater = 4
	// HighWater is the maximum number of connections (D_hi)
	HighWater = 8
	// GracePeriod is how long to wait before pruning connections
	GracePeriod = 0
)

type Protocol interface {
	HandleStream(network.Stream)
}

type Node struct {
	host      host.Host
	ctx       context.Context
	peerMgr   *PeerManager
	ps        *pubsub.PubSub
	protocol  Protocol
	protoLock sync.RWMutex
}

func NewNode(ctx context.Context) (*Node, error) {
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, 2048, rand.Reader)
	if err != nil {
		return nil, err
	}

	// Create connection manager with D_lo and D_hi limits
	cm, err := connmgr.NewConnManager(LowWater, HighWater, connmgr.WithGracePeriod(GracePeriod))
	if err != nil {
		return nil, err
	}

	opts := []libp2p.Option{
		libp2p.Identity(priv),
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"),
		libp2p.DisableRelay(),
		libp2p.NATPortMap(),
		libp2p.EnableNATService(),
		libp2p.ConnectionManager(cm), // Limit connections
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return nil, err
	}

	// Initialize GossipSub with explicit mesh parameters
	ps, err := pubsub.New(ctx, h)
	if err != nil {
		h.Close()
		return nil, err
	}

	n := &Node{
		host:    h,
		ctx:     ctx,
		peerMgr: NewPeerManager(HighWater), // Track up to HighWater peers
		ps:      ps,
	}

	h.Network().Notify(&network.NotifyBundle{
		DisconnectedF: func(_ network.Network, c network.Conn) {
			n.peerMgr.Remove(c.RemotePeer())
		},
	})

	logger.Debug("Node created with connection limits: low=%d, high=%d", LowWater, HighWater)
	return n, nil
}

func (n *Node) SetProtocol(p Protocol) {
	n.protoLock.Lock()
	n.protocol = p
	n.protoLock.Unlock()
}

func (n *Node) Host() host.Host {
	return n.host
}

func (n *Node) ID() peer.ID {
	return n.host.ID()
}

func (n *Node) Addrs() []multiaddr.Multiaddr {
	return n.host.Addrs()
}

func (n *Node) Context() context.Context {
	return n.ctx
}

func (n *Node) PeerManager() *PeerManager {
	return n.peerMgr
}

// PubSub returns the GossipSub instance
func (n *Node) PubSub() *pubsub.PubSub {
	return n.ps
}

// ListPeers logs peer list (for debug) and returns formatted string
func (n *Node) ListPeers() string {
	peers := n.peerMgr.List()
	if len(peers) == 0 {
		logger.Debugln("No connected peers")
		return "No connected peers"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Connected peers (%d/%d):\n", len(peers), HighWater))
	for _, p := range peers {
		sb.WriteString(fmt.Sprintf("  - %s\n", p.String()))
	}

	result := sb.String()
	logger.Debug("%s", result)
	return result
}
