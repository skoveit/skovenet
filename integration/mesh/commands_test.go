//go:build integration && !windows

package mesh_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/skoveit/skovenet/pkg/command"
	"github.com/skoveit/skovenet/pkg/node"
	"github.com/skoveit/skovenet/pkg/protocol"
	"github.com/skoveit/skovenet/pkg/pubsub"
	"github.com/skoveit/skovenet/pkg/signing"
)

// This public fixture is used only by this test binary, through an explicit
// linker flag. No production operator key or trust configuration is changed.
const fixturePublicKey = "/rSMnRsuFuRc/hqy/QPe98h++DMjZQnolH1eNAA866Q="

func fixturePrivateKey() string {
	seed := sha256.Sum256([]byte("SkoveNet integration-only operator fixture v1"))
	return base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(seed[:]))
}

func requireFixtureTrust(t *testing.T) {
	t.Helper()
	if signing.GetPublicKeyBase64() != fixturePublicKey {
		t.Fatalf("this suite requires its integration-only trust key; run go test -tags=integration -ldflags '-X github.com/skoveit/skovenet/pkg/signing.publicKeyB64=%s' ./integration/mesh", fixturePublicKey)
	}
}

type commandResponse struct {
	source, output, commandID string
}

type commandNode struct {
	node      *node.Node
	protocol  *protocol.Protocol
	responses chan commandResponse
}

func newCommandNode(t *testing.T) *commandNode {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	h, err := libp2p.New(
		libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"),
		libp2p.DisableRelay(),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close() })
	n, err := node.NewNodeWithHost(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.PubSub().Close() })
	responses := make(chan commandResponse, 64)
	handler := command.NewHandler(n)
	p := protocol.NewProtocol(n, handler)
	handler.SetProtocol(p)
	handler.SetResponseCallback(func(source, output, commandID string) {
		select {
		case responses <- commandResponse{source, output, commandID}:
		default:
			t.Error("test response queue overflowed")
		}
	})
	n.SetProtocol(p)
	t.Cleanup(cancel)
	waitFor(t, "protocol subscription", func() bool {
		return n.PubSub().Subscription(pubsub.DefaultTopic) != nil
	})
	return &commandNode{node: n, protocol: p, responses: responses}
}

func waitFor(t *testing.T, description string, ready func() bool) {
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

func hasPeer(ids []peer.ID, want peer.ID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func connectNodes(t *testing.T, a, b *commandNode) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.node.Host().Connect(ctx, peer.AddrInfo{ID: b.node.ID(), Addrs: b.node.Addrs()}); err != nil {
		t.Fatal(err)
	}
	a.node.PeerManager().Add(b.node.ID())
	b.node.PeerManager().Add(a.node.ID())
	waitFor(t, "mutual topic subscriptions", func() bool {
		return hasPeer(a.node.PubSub().ListPeers(pubsub.DefaultTopic), b.node.ID()) &&
			hasPeer(b.node.PubSub().ListPeers(pubsub.DefaultTopic), a.node.ID())
	})
}

func waitForMesh(t *testing.T, source, target *commandNode) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		source.protocol.SendResponseWithCmdID(target.node.ID().String(), "mesh-ready", "readiness-probe")
		select {
		case result := <-target.responses:
			if result.output == "mesh-ready" && result.commandID == "readiness-probe" {
				return
			}
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("GossipSub mesh did not deliver a readiness probe")
		}
	}
}

func awaitResponse(t *testing.T, n *commandNode, commandID string) commandResponse {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case result := <-n.responses:
			if result.commandID == commandID {
				return result
			}
		case <-deadline.C:
			t.Fatalf("no correlated response for command %q", commandID)
		}
	}
}

func TestMesh_AuthorizedCommandDirectAndMultiHop(t *testing.T) {
	requireFixtureTrust(t)
	for _, multiHop := range []bool{false, true} {
		name := "direct"
		if multiHop {
			name = "multi-hop"
		}
		t.Run(name, func(t *testing.T) {
			operator, target := newCommandNode(t), newCommandNode(t)
			if multiHop {
				relay := newCommandNode(t)
				connectNodes(t, operator, relay)
				connectNodes(t, relay, target)
				waitForMesh(t, operator, target)
				waitForMesh(t, target, operator)
				if operator.node.Host().Network().Connectedness(target.node.ID()) == network.Connected ||
					operator.node.PeerManager().Has(target.node.ID()) {
					t.Fatal("multi-hop case unexpectedly has a direct source-to-target connection")
				}
			} else {
				connectNodes(t, operator, target)
			}
			operator.protocol.SetPrivateKey(fixturePrivateKey())
			workingDirectory, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			commandID := operator.protocol.SendCommand(target.node.ID().String(), "pwd")
			if commandID == "" {
				t.Fatal("signed command did not return an ID")
			}
			result := awaitResponse(t, operator, commandID)
			if result.source != target.node.ID().String() || result.output != workingDirectory {
				t.Fatalf("incorrect command result: %+v, want output %q from target", result, workingDirectory)
			}

			fixtureDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(fixtureDir, "mesh-fixture.txt"), []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			commandID = operator.protocol.SendCommand(target.node.ID().String(), "ls "+fixtureDir)
			if commandID == "" {
				t.Fatal("second signed command did not return an ID")
			}
			result = awaitResponse(t, operator, commandID)
			if result.source != target.node.ID().String() || !strings.Contains(result.output, "mesh-fixture.txt") {
				t.Fatalf("directory command did not return the remote fixture: %+v", result)
			}
			if multiHop && operator.node.Host().Network().Connectedness(target.node.ID()) == network.Connected {
				t.Fatal("command flow established a direct connection in the multi-hop case")
			}
		})
	}
}

func sendRawMessage(t *testing.T, source, target *commandNode, msg *protocol.Message) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := source.node.Host().NewStream(ctx, target.node.ID(), protocol.ProtocolID)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	data, err := msg.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := s.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	// The receiver closes its stream only after dispatch has returned. Waiting
	// for EOF makes the non-execution check deterministic rather than a sleep.
	if _, err := io.Copy(io.Discard, s); err != nil {
		t.Fatalf("wait for receiver to finish: %v", err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func TestMesh_UnauthorizedMessagesCannotExecute(t *testing.T) {
	requireFixtureTrust(t)
	operator, target := newCommandNode(t), newCommandNode(t)
	connectNodes(t, operator, target)
	wrongSeed := sha256.Sum256([]byte("SkoveNet integration-only UNTRUSTED fixture v1"))
	wrongKey := base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(wrongSeed[:]))
	tests := []struct {
		name    string
		msgType protocol.MessageType
		signKey string
		tamper  bool
	}{
		{"unsigned command", protocol.MsgTypeCommand, "", false},
		{"untrusted operator", protocol.MsgTypeCommand, wrongKey, false},
		{"tampered command", protocol.MsgTypeCommand, fixturePrivateKey(), true},
		{"route message", protocol.MsgTypeRoute, "", false},
		{"ping message", protocol.MsgTypePing, "", false},
		{"pong message", protocol.MsgTypePong, "", false},
		{"topology request", protocol.MsgTypeTopoReq, "", false},
		{"topology response", protocol.MsgTypeTopoRes, "", false},
		{"unknown message", protocol.MessageType("unknown"), "", false},
		{"empty message type", protocol.MessageType(""), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "must-not-exist")
			payload := "printf executed > " + shellQuote(marker)
			msg := protocol.NewMessage(tt.msgType, operator.node.ID().String(), target.node.ID().String(), payload)
			if tt.signKey != "" {
				if err := msg.SignWithKey(tt.signKey); err != nil {
					t.Fatal(err)
				}
			}
			if tt.tamper {
				msg.Payload += " # changed after signing"
			}
			sendRawMessage(t, operator, target, msg)
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("unauthorized payload executed or fixture check failed: %v", err)
			}
		})
	}
}
