//go:build integration

package protocol

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/skoveit/skovenet/pkg/node"
	"github.com/skoveit/skovenet/pkg/pubsub"
)

type integrationHandler struct {
	messages chan *Message
}

func (h *integrationHandler) Handle(msg *Message) error {
	select {
	case h.messages <- msg:
		return nil
	default:
		return fmt.Errorf("test handler received too many messages")
	}
}

type integrationNode struct {
	node     *node.Node
	protocol *Protocol
	handler  *integrationHandler
}

func newIntegrationNode(t *testing.T) *integrationNode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.DisableRelay(),
	)
	if err != nil {
		t.Fatalf("create loopback host: %v", err)
	}
	t.Cleanup(func() { h.Close() })
	n, err := node.NewNodeWithHost(ctx, h)
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	t.Cleanup(func() { n.PubSub().Close() })
	handler := &integrationHandler{messages: make(chan *Message, 64)}
	p := NewProtocol(n, handler)
	t.Cleanup(cancel)
	waitIntegration(t, "protocol subscription", func() bool {
		return n.PubSub().Subscription(pubsub.DefaultTopic) != nil
	})
	return &integrationNode{node: n, protocol: p, handler: handler}
}

func connectIntegrationNodes(t *testing.T, a, b *integrationNode) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.node.Host().Connect(ctx, peer.AddrInfo{ID: b.node.ID(), Addrs: b.node.Addrs()}); err != nil {
		t.Fatalf("connect loopback peers: %v", err)
	}
	a.node.PeerManager().Add(b.node.ID())
	b.node.PeerManager().Add(a.node.ID())
	waitIntegration(t, "mutual topic subscriptions", func() bool {
		return containsIntegrationPeer(a.node.PubSub().ListPeers(pubsub.DefaultTopic), b.node.ID()) &&
			containsIntegrationPeer(b.node.PubSub().ListPeers(pubsub.DefaultTopic), a.node.ID())
	})
}

func containsIntegrationPeer(peers []peer.ID, want peer.ID) bool {
	for _, id := range peers {
		if id == want {
			return true
		}
	}
	return false
}

func waitIntegration(t *testing.T, description string, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if ready() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s", description)
		case <-tick.C:
		}
	}
}

func receiveIntegrationPayload(t *testing.T, messages <-chan *Message, payload string) *Message {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case msg := <-messages:
			if msg.Payload == payload {
				return msg
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for payload %q", payload)
		}
	}
}

func waitIntegrationMesh(t *testing.T, source, target *integrationNode) {
	t.Helper()
	payload := "mesh-ready-" + generateID()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		source.protocol.publishMessage(NewMessage(MsgTypeResponse, source.node.ID().String(), target.node.ID().String(), payload))
		select {
		case msg := <-target.handler.messages:
			if msg.Payload == payload && msg.Source == source.node.ID().String() {
				return
			}
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("GossipSub mesh did not deliver a readiness probe")
		}
	}
}

func TestProtocolIntegration_DirectResponseCorrelation(t *testing.T) {
	source, target := newIntegrationNode(t), newIntegrationNode(t)
	connectIntegrationNodes(t, source, target)
	source.protocol.SendResponseWithCmdID(target.node.ID().String(), "direct-result", "command-123")
	msg := receiveIntegrationPayload(t, target.handler.messages, "direct-result")
	if msg.Type != MsgTypeResponse || msg.Source != source.node.ID().String() ||
		msg.Target != target.node.ID().String() || msg.CmdID != "command-123" {
		t.Fatalf("incorrect direct response envelope: %+v", msg)
	}
	if !msg.HasVisited(target.node.ID()) || msg.TTL != 9 {
		t.Fatalf("direct stream did not record the target hop: %+v", msg)
	}
}

func TestProtocolIntegration_GossipSubMultiHopResponse(t *testing.T) {
	source, relay, target := newIntegrationNode(t), newIntegrationNode(t), newIntegrationNode(t)
	connectIntegrationNodes(t, source, relay)
	connectIntegrationNodes(t, relay, target)
	waitIntegrationMesh(t, source, target)
	if source.node.Host().Network().Connectedness(target.node.ID()) == network.Connected ||
		source.node.PeerManager().Has(target.node.ID()) {
		t.Fatal("test requires source and target to communicate through the relay")
	}

	source.protocol.SendResponseWithCmdID(target.node.ID().String(), "multi-hop-result", "command-456")
	msg := receiveIntegrationPayload(t, target.handler.messages, "multi-hop-result")
	if msg.Type != MsgTypeResponse || msg.Source != source.node.ID().String() ||
		msg.Target != target.node.ID().String() || msg.CmdID != "command-456" {
		t.Fatalf("incorrect GossipSub response envelope: %+v", msg)
	}
	if source.node.Host().Network().Connectedness(target.node.ID()) == network.Connected {
		t.Fatal("source connected directly to the target during the multi-hop test")
	}
}

func TestProtocolIntegration_RadarAndTopology(t *testing.T) {
	operator, relay, target := newIntegrationNode(t), newIntegrationNode(t), newIntegrationNode(t)
	connectIntegrationNodes(t, operator, relay)
	connectIntegrationNodes(t, relay, target)
	waitIntegrationMesh(t, operator, target)
	waitIntegrationMesh(t, target, operator)

	type pong struct{ source, payload string }
	pongs := make(chan pong, 8)
	operator.protocol.SetPongCallback(func(source, payload string) {
		pongs <- pong{source, payload}
	})
	operator.protocol.Broadcast("radar-123")
	seen := make(map[string]bool)
	correlation := ""
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for len(seen) < 2 {
		select {
		case result := <-pongs:
			// Current radar replies identify the ping's envelope ID. Every
			// responder must refer to the same request.
			if result.payload == "" || (correlation != "" && result.payload != correlation) {
				t.Fatalf("incorrect radar correlation: %+v", result)
			}
			correlation = result.payload
			if result.source != relay.node.ID().String() && result.source != target.node.ID().String() {
				t.Fatalf("radar returned an unexpected peer: %s", result.source)
			}
			seen[result.source] = true
		case <-deadline.C:
			t.Fatalf("radar found %d of 2 peers", len(seen))
		}
	}

	type topology struct {
		source string
		peers  []string
	}
	topologies := make(chan topology, 8)
	operator.protocol.SetTopoCallback(func(source string, peers []string) {
		topologies <- topology{source, peers}
	})
	operator.protocol.BroadcastTopology("topology-123")
	seen = make(map[string]bool)
	deadline.Reset(5 * time.Second)
	for len(seen) < 2 {
		select {
		case result := <-topologies:
			switch result.source {
			case relay.node.ID().String():
				if len(result.peers) != 2 || !containsIntegrationID(result.peers, operator.node.ID().String()) ||
					!containsIntegrationID(result.peers, target.node.ID().String()) {
					t.Fatalf("relay reported incorrect neighbors: %v", result.peers)
				}
			case target.node.ID().String():
				if len(result.peers) != 1 || result.peers[0] != relay.node.ID().String() {
					t.Fatalf("target reported incorrect neighbors: %v", result.peers)
				}
			default:
				t.Fatalf("topology returned an unexpected peer: %s", result.source)
			}
			seen[result.source] = true
		case <-deadline.C:
			t.Fatalf("topology returned %d of 2 peers", len(seen))
		}
	}
}

func containsIntegrationID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// inputStream exercises the actual direct-stream decoder synchronously so a
// rejected message can be asserted without a timing-dependent negative wait.
type integrationInputStream struct {
	network.Stream
	reader *bytes.Reader
	closed bool
}

func (s *integrationInputStream) Read(data []byte) (int, error) {
	return s.reader.Read(data)
}

func (s *integrationInputStream) Close() error {
	s.closed = true
	return nil
}

func TestProtocolIntegration_RejectsInvalidCommandSignatures(t *testing.T) {
	target := newIntegrationNode(t)
	_, untrustedKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		sign func(*Message)
	}{
		{"unsigned", func(*Message) {}},
		{"malformed signature", func(m *Message) { m.Signature = "not-base64" }},
		{"wrong signature size", func(m *Message) { m.Signature = base64.StdEncoding.EncodeToString([]byte("short")) }},
		{"untrusted signing key", func(m *Message) {
			if err := m.SignWithKey(base64.StdEncoding.EncodeToString(untrustedKey)); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, transport := range []string{"direct", "gossip"} {
		for _, tt := range tests {
			t.Run(transport+"/"+tt.name, func(t *testing.T) {
				msg := NewMessage(MsgTypeCommand, "untrusted-peer", target.node.ID().String(), "must-not-dispatch")
				tt.sign(msg)
				data, err := msg.Marshal()
				if err != nil {
					t.Fatal(err)
				}
				if transport == "direct" {
					stream := &integrationInputStream{reader: bytes.NewReader(append(data, '\n'))}
					target.protocol.HandleStream(stream)
					if !stream.closed {
						t.Error("direct stream was not closed")
					}
				} else {
					target.protocol.handlePubSubMessage(data)
				}
				select {
				case received := <-target.handler.messages:
					t.Fatalf("invalid command reached handler: %+v", received)
				default:
				}
			})
		}
	}
}

func TestProtocolIntegration_IgnoresMalformedVisitedAndForeignMessages(t *testing.T) {
	target := newIntegrationNode(t)
	visited := NewMessage(MsgTypeResponse, "remote-peer", target.node.ID().String(), "already-visited")
	visited.AddVisited(target.node.ID())
	foreign := NewMessage(MsgTypeResponse, "remote-peer", "another-peer", "not-for-us")
	foreign.TTL = 0
	for _, transport := range []string{"direct", "gossip"} {
		t.Run(transport, func(t *testing.T) {
			visitedData, _ := visited.Marshal()
			foreignData, _ := foreign.Marshal()
			for _, data := range [][]byte{[]byte("not-json"), visitedData, foreignData} {
				if transport == "direct" {
					target.protocol.HandleStream(&integrationInputStream{reader: bytes.NewReader(data)})
				} else {
					target.protocol.handlePubSubMessage(data)
				}
			}
			select {
			case received := <-target.handler.messages:
				t.Fatalf("ignored message reached handler: %+v", received)
			default:
			}
		})
	}
}
