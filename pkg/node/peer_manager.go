package node

import (
	"sync"
	"time"

	"github.com/skoveit/skovenet/pkg/logger"

	"github.com/libp2p/go-libp2p/core/peer"
)

// PeerCallback is called when a peer connects or disconnects
type PeerCallback func(peerID string, connected bool)

type PeerManager struct {
	peers    map[peer.ID]time.Time
	maxPeers int
	callback PeerCallback
	mu       sync.RWMutex
}

func NewPeerManager(maxPeers int) *PeerManager {
	return &PeerManager{
		peers:    make(map[peer.ID]time.Time),
		maxPeers: maxPeers,
	}
}

func (pm *PeerManager) SetCallback(cb PeerCallback) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.callback = cb
}

func (pm *PeerManager) Add(p peer.ID) bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.peers[p]; exists {
		return false
	}

	if len(pm.peers) >= pm.maxPeers {
		return false
	}

	pm.peers[p] = time.Now()
	logger.Info("Peer connected [%d/%d]: %s", len(pm.peers), pm.maxPeers, p.String())

	if pm.callback != nil {
		go pm.callback(p.String(), true)
	}
	return true
}

func (pm *PeerManager) Remove(p peer.ID) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if _, exists := pm.peers[p]; exists {
		delete(pm.peers, p)
		logger.Info("Peer disconnected [%d/%d]: %s", len(pm.peers), pm.maxPeers, p.String())

		if pm.callback != nil {
			go pm.callback(p.String(), false)
		}
	}
}

func (pm *PeerManager) Has(p peer.ID) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	_, exists := pm.peers[p]
	return exists
}

func (pm *PeerManager) IsFull() bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.peers) >= pm.maxPeers
}

func (pm *PeerManager) Count() int {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return len(pm.peers)
}

func (pm *PeerManager) List() []peer.ID {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	peers := make([]peer.ID, 0, len(pm.peers))
	for p := range pm.peers {
		peers = append(peers, p)
	}
	return peers
}

func (pm *PeerManager) ParsePeer(peerID string) (peer.ID, error) {
	return peer.Decode(peerID)
}
