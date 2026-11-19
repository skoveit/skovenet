//go:build integration

package protocol

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/network"
)

func TestProtocolIntegration_RelayLossUsesAlternatePath(t *testing.T) {
	source, firstRelay, target := newIntegrationNode(t), newIntegrationNode(t), newIntegrationNode(t)
	connectIntegrationNodes(t, source, firstRelay)
	connectIntegrationNodes(t, firstRelay, target)
	waitIntegrationMesh(t, source, target)
	assertNoDirectConnection(t, source, target)

	// Add a second path without connecting the source and target directly.
	standby := newIntegrationNode(t)
	connectIntegrationNodes(t, source, standby)
	connectIntegrationNodes(t, standby, target)
	waitIntegrationMesh(t, standby, source)
	waitIntegrationMesh(t, standby, target)

	// Losing the original relay must leave both directions usable.
	if err := firstRelay.node.Host().Close(); err != nil {
		t.Fatalf("close original relay: %v", err)
	}
	waitIntegration(t, "original relay disconnection", func() bool {
		return !source.node.PeerManager().Has(firstRelay.node.ID()) &&
			!target.node.PeerManager().Has(firstRelay.node.ID())
	})
	waitIntegrationMesh(t, source, target)
	waitIntegrationMesh(t, target, source)
	assertNoDirectConnection(t, source, target)

	for _, direction := range []struct {
		name         string
		from, to     *integrationNode
		commandID    string
		responseText string
	}{
		{"forward", source, target, "after-relay-loss-forward", "forward-result"},
		{"reverse", target, source, "after-relay-loss-reverse", "reverse-result"},
	} {
		t.Run(direction.name, func(t *testing.T) {
			direction.from.protocol.SendResponseWithCmdID(direction.to.node.ID().String(), direction.responseText, direction.commandID)
			msg := receiveIntegrationPayload(t, direction.to.handler.messages, direction.responseText)
			if msg.Type != MsgTypeResponse || msg.CmdID != direction.commandID ||
				msg.Source != direction.from.node.ID().String() || msg.Target != direction.to.node.ID().String() {
				t.Fatalf("response lost its correlation after relay loss: %+v", msg)
			}
			assertNoDirectConnection(t, source, target)
		})
	}
}

func assertNoDirectConnection(t *testing.T, source, target *integrationNode) {
	t.Helper()
	if source.node.Host().Network().Connectedness(target.node.ID()) == network.Connected ||
		target.node.Host().Network().Connectedness(source.node.ID()) == network.Connected ||
		source.node.PeerManager().Has(target.node.ID()) || target.node.PeerManager().Has(source.node.ID()) {
		t.Fatal("verification requires delivery through relay nodes, not a direct endpoint connection")
	}
}
