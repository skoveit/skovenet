package protocol

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/skoveit/skovenet/pkg/logger"
	"github.com/skoveit/skovenet/pkg/node"
	"github.com/skoveit/skovenet/pkg/pubsub"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

const ProtocolID = "/mesh-c2/1.0.0"

type CommandHandler interface {
	Handle(msg *Message) error
}

// ResponseCallback is called when a response is received
type ResponseCallback func(source, payload string)

// PongCallback is called when a radar pong is received
type PongCallback func(peerID, payload string)

// TopoCallback is called when a topology response is received (peerID, list of that peer's connections)
type TopoCallback func(peerID string, peers []string)

type Protocol struct {
	node             *node.Node
	handler          CommandHandler
	responseCallback ResponseCallback
	pongCallback     PongCallback
	topoCallback     TopoCallback
	privateKey       string // Operator's private key for signing commands
	callbackMu       sync.RWMutex
}

func NewProtocol(n *node.Node, handler CommandHandler) *Protocol {
	p := &Protocol{
		node:    n,
		handler: handler,
	}

	// Keep direct stream handler for targeted messages
	n.Host().SetStreamHandler(ProtocolID, p.HandleStream)

	// Subscribe to GossipSub topic and start listening
	go p.startPubSubListener()

	return p
}

// startPubSubListener subscribes to the mesh topic and processes incoming messages
func (p *Protocol) startPubSubListener() {
	ps := p.node.PubSub()
	if ps == nil {
		logger.Debug("PubSub not initialized, skipping listener")
		return
	}

	sub, err := ps.Join(pubsub.DefaultTopic)
	if err != nil {
		logger.Debug("Failed to join pubsub topic: %v", err)
		return
	}

	logger.Debug("Subscribed to GossipSub topic: %s", pubsub.DefaultTopic)

	for {
		msg, err := sub.Next(p.node.Context())
		if err != nil {
			// Context cancelled or subscription closed
			return
		}

		// Ignore messages from self
		if msg.ReceivedFrom == p.node.ID() {
			continue
		}

		p.handlePubSubMessage(msg.Data)
	}
}

// handlePubSubMessage processes a message received via GossipSub
func (p *Protocol) handlePubSubMessage(data []byte) {
	msg, err := UnmarshalMessage(data)
	if err != nil {
		return
	}

	// Check if message already visited this node (loop prevention)
	if msg.HasVisited(p.node.ID()) {
		return
	}

	// Handle broadcast messages (ping/pong for radar, topology for graph)
	switch msg.Type {
	case MsgTypePing:
		// Respond to radar ping with pong
		logger.Debug("📡 Radar ping from %s", msg.Source)
		p.sendPong(msg.Source, msg.ID)
		return
	case MsgTypePong:
		// Forward pong to callback
		p.callbackMu.RLock()
		cb := p.pongCallback
		p.callbackMu.RUnlock()
		if cb != nil {
			cb(msg.Source, msg.Payload)
		}
		return
	case MsgTypeTopoReq:
		// Respond with our peer list
		logger.Debug("🗺️ Topology request from %s", msg.Source)
		p.sendTopologyResponse(msg.Source, msg.ID)
		return
	case MsgTypeTopoRes:
		// Forward topology response to callback
		p.callbackMu.RLock()
		cb := p.topoCallback
		p.callbackMu.RUnlock()
		if cb != nil {
			// Payload is JSON array of peer IDs
			var peers []string
			if err := json.Unmarshal([]byte(msg.Payload), &peers); err == nil {
				cb(msg.Source, peers)
			}
		}
		return
	}

	// Check if message is for this node
	if msg.Target == p.node.ID().String() {
		// Verify signature for command messages
		if msg.Type == MsgTypeCommand {
			if !msg.VerifySignature() {
				logger.Debug("🚫 Rejected unsigned/invalid command from %s", msg.Source)
				return
			}
			logger.Debug("✅ Signature verified for command from %s", msg.Source)
		}

		logger.Debug("📩 [GossipSub] Received %s", msg.Type.String())
		if err := p.handler.Handle(msg); err != nil {
			logger.Debug("Error handling command: %v", err)
		}
	}
	// No need to re-route — GossipSub handles propagation
}

// SetResponseCallback sets a callback for when responses are received
func (p *Protocol) SetResponseCallback(cb ResponseCallback) {
	p.callbackMu.Lock()
	defer p.callbackMu.Unlock()
	p.responseCallback = cb
}

// SetPongCallback sets a callback for radar pong responses
func (p *Protocol) SetPongCallback(cb PongCallback) {
	p.callbackMu.Lock()
	defer p.callbackMu.Unlock()
	p.pongCallback = cb
}

// Broadcast sends a ping to all nodes in the network (for radar)
func (p *Protocol) Broadcast(pingID string) {
	msg := NewMessage(MsgTypePing, p.node.ID().String(), "*", pingID)
	logger.Debug("📡 Broadcasting radar ping: %s", pingID)
	p.publishMessage(msg)
}

// sendPong responds to a radar ping
func (p *Protocol) sendPong(targetID, pingID string) {
	msg := NewMessage(MsgTypePong, p.node.ID().String(), targetID, pingID)
	p.publishMessage(msg)
}

// SetTopoCallback sets a callback for topology responses
func (p *Protocol) SetTopoCallback(cb TopoCallback) {
	p.callbackMu.Lock()
	defer p.callbackMu.Unlock()
	p.topoCallback = cb
}

// SetPrivateKey sets the operator's private key for signing commands.
// The key should be a base64-encoded Ed25519 private key.
func (p *Protocol) SetPrivateKey(key string) {
	p.callbackMu.Lock()
	defer p.callbackMu.Unlock()
	p.privateKey = key
}

// HasPrivateKey returns true if a private key has been set.
func (p *Protocol) HasPrivateKey() bool {
	p.callbackMu.RLock()
	defer p.callbackMu.RUnlock()
	return p.privateKey != ""
}

// BroadcastTopology sends a topology request to all nodes
func (p *Protocol) BroadcastTopology(reqID string) {
	msg := NewMessage(MsgTypeTopoReq, p.node.ID().String(), "*", reqID)
	logger.Debug("🗺️ Broadcasting topology request: %s", reqID)
	p.publishMessage(msg)
}

// sendTopologyResponse responds to a topology request with our peer list
func (p *Protocol) sendTopologyResponse(targetID, reqID string) {
	peers := p.node.PeerManager().List()
	peerIDs := make([]string, len(peers))
	for i, pid := range peers {
		peerIDs[i] = pid.String()
	}
	payload, _ := json.Marshal(peerIDs)
	msg := NewMessage(MsgTypeTopoRes, p.node.ID().String(), targetID, string(payload))
	p.publishMessage(msg)
}

// HandleStream handles direct stream messages (backward compatibility)
func (p *Protocol) HandleStream(s network.Stream) {
	defer s.Close()

	reader := bufio.NewReader(s)
	data, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return
	}

	msg, err := UnmarshalMessage(data)
	if err != nil {
		return
	}

	// Check if message already visited this node
	if msg.HasVisited(p.node.ID()) {
		return
	}

	msg.AddVisited(p.node.ID())

	// Check if message is for this node
	if msg.Target == p.node.ID().String() {
		// Verify signature for command messages
		if msg.Type == MsgTypeCommand {
			if !msg.VerifySignature() {
				logger.Debug("🚫 Rejected unsigned/invalid command from %s", msg.Source)
				return
			}
			logger.Debug("✅ Signature verified for command from %s", msg.Source)
		}

		logger.Debug("📩 [Direct] Received %s", msg.Type.String())
		if err := p.handler.Handle(msg); err != nil {
			logger.Debug("Error handling command: %v", err)
		}
		return
	}

	// Forward via GossipSub if not for us
	if msg.TTL > 0 {
		p.publishMessage(msg)
	}
}

func (p *Protocol) Send(msgType MessageType, targetID, payload string) {
	msg := NewMessage(msgType, p.node.ID().String(), targetID, payload)

	// Sign command messages
	if msgType == MsgTypeCommand {
		p.callbackMu.RLock()
		privKey := p.privateKey
		p.callbackMu.RUnlock()

		if privKey == "" {
			logger.Debug("❌ Cannot send command: not signed in (use 'sign' command)")
			return
		}

		if err := msg.SignWithKey(privKey); err != nil {
			logger.Debug("❌ Failed to sign command: %v", err)
			return
		}
		logger.Debug("✍️ Signed command to %s", targetID)
	}

	// Try direct connection first if peer is known
	target, err := peer.Decode(targetID)
	if err != nil {
		logger.Debug("Invalid peer ID: %v", err)
		return
	}

	if p.node.PeerManager().Has(target) {
		if err := p.sendDirect(target, msg); err == nil {
			logger.Debug("📤 %s sent directly to %s", msgType.String(), targetID)
			return
		}
	}

	// Broadcast via GossipSub
	logger.Debug("📡 Broadcasting %s via GossipSub to %s", msgType.String(), targetID)
	p.publishMessage(msg)
}

// publishMessage publishes a message to the GossipSub topic
func (p *Protocol) publishMessage(msg *Message) {
	ps := p.node.PubSub()
	if ps == nil {
		return
	}

	data, err := msg.Marshal()
	if err != nil {
		logger.Debug("Failed to marshal message: %v", err)
		return
	}

	if err := ps.Publish(pubsub.DefaultTopic, data); err != nil {
		logger.Debug("Failed to publish message: %v", err)
	}
}

// SendCommand is a convenience wrapper for sending commands
func (p *Protocol) SendCommand(targetID, command string) {
	p.Send(MsgTypeCommand, targetID, command)
}

// SendResponse is a convenience wrapper for sending responses
func (p *Protocol) SendResponse(targetID, response string) {
	p.Send(MsgTypeResponse, targetID, response)
}

// SendResponseWithCmdID sends a response that includes the originating command's ID
// for correlation on the operator's side.
func (p *Protocol) SendResponseWithCmdID(targetID, response, cmdID string) {
	msg := NewMessage(MsgTypeResponse, p.node.ID().String(), targetID, response)
	msg.CmdID = cmdID

	// Try direct connection first
	target, err := peer.Decode(targetID)
	if err != nil {
		logger.Debug("Invalid peer ID: %v", err)
		return
	}

	if p.node.PeerManager().Has(target) {
		if err := p.sendDirect(target, msg); err == nil {
			logger.Debug("📤 response sent directly to %s (cmd: %s)", targetID, cmdID)
			return
		}
	}

	logger.Debug("📡 Broadcasting response via GossipSub to %s (cmd: %s)", targetID, cmdID)
	p.publishMessage(msg)
}

// sendDirect sends a message directly to a peer via stream
func (p *Protocol) sendDirect(target peer.ID, msg *Message) error {
	ctx, cancel := context.WithTimeout(p.node.Context(), 5*time.Second)
	defer cancel()

	s, err := p.node.Host().NewStream(ctx, target, ProtocolID)
	if err != nil {
		return err
	}
	defer s.Close()

	data, err := msg.Marshal()
	if err != nil {
		return err
	}

	data = append(data, '\n')
	_, err = s.Write(data)
	return err
}
