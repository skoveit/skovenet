package pubsub

import (
	"context"
	"sync"

	"github.com/skoveit/skovenet/pkg/logger"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	// DefaultTopic is the main topic for C2 messages
	DefaultTopic = "mesh-c2/messages/1.0.0"
)

// GossipSub mesh parameters
const (
	GossipSubD   = 6  // Target mesh degree
	GossipSubDlo = 4  // Lower bound for mesh degree
	GossipSubDhi = 8 // Upper bound for mesh degree
)

// PubSub wraps libp2p GossipSub for mesh networking
type PubSub struct {
	ctx    context.Context
	ps     *pubsub.PubSub
	host   host.Host
	topics map[string]*pubsub.Topic
	subs   map[string]*pubsub.Subscription
	mu     sync.RWMutex
}

// New creates a new GossipSub-based PubSub instance with explicit mesh parameters
func New(ctx context.Context, h host.Host) (*PubSub, error) {
	// Configure GossipSub with explicit mesh parameters
	ps, err := pubsub.NewGossipSub(ctx, h,
		pubsub.WithPeerExchange(true),              // Enable peer exchange for discovery
		pubsub.WithFloodPublish(false),             // Don't flood, use mesh routing
		pubsub.WithGossipSubParams(gossipParams()), // Custom mesh parameters
	)
	if err != nil {
		return nil, err
	}

	p := &PubSub{
		ctx:    ctx,
		ps:     ps,
		host:   h,
		topics: make(map[string]*pubsub.Topic),
		subs:   make(map[string]*pubsub.Subscription),
	}

	logger.Debug("GossipSub initialized with D=%d, D_lo=%d, D_hi=%d", GossipSubD, GossipSubDlo, GossipSubDhi)
	return p, nil
}

// gossipParams returns custom GossipSub parameters
func gossipParams() pubsub.GossipSubParams {
	params := pubsub.DefaultGossipSubParams()
	params.D = GossipSubD     // Target degree: 6 peers in mesh
	params.Dlo = GossipSubDlo // Min degree: 4 peers
	params.Dhi = GossipSubDhi // Max degree: 8 peers
	return params
}

// Join joins a topic and subscribes to it
func (p *PubSub) Join(topicName string) (*pubsub.Subscription, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return existing subscription if already joined
	if sub, exists := p.subs[topicName]; exists {
		return sub, nil
	}

	topic, err := p.ps.Join(topicName)
	if err != nil {
		return nil, err
	}

	sub, err := topic.Subscribe()
	if err != nil {
		return nil, err
	}

	p.topics[topicName] = topic
	p.subs[topicName] = sub

	logger.Debug("Joined topic: %s", topicName)
	return sub, nil
}

// Publish sends data to a topic
func (p *PubSub) Publish(topicName string, data []byte) error {
	p.mu.RLock()
	topic, exists := p.topics[topicName]
	p.mu.RUnlock()

	if !exists {
		// Auto-join if not already joined
		p.mu.Lock()
		var err error
		topic, err = p.ps.Join(topicName)
		if err != nil {
			p.mu.Unlock()
			return err
		}
		p.topics[topicName] = topic
		p.mu.Unlock()
	}

	return topic.Publish(p.ctx, data)
}

// Subscription returns the subscription for a topic, or nil if not subscribed
func (p *PubSub) Subscription(topicName string) *pubsub.Subscription {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.subs[topicName]
}

// ListPeers returns the peers subscribed to a topic
func (p *PubSub) ListPeers(topicName string) []peer.ID {
	p.mu.RLock()
	topic, exists := p.topics[topicName]
	p.mu.RUnlock()

	if !exists {
		return nil
	}
	return topic.ListPeers()
}

// Close cleans up all subscriptions and topics
func (p *PubSub) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, sub := range p.subs {
		sub.Cancel()
	}
	for _, topic := range p.topics {
		topic.Close()
	}

	p.subs = make(map[string]*pubsub.Subscription)
	p.topics = make(map[string]*pubsub.Topic)
}
