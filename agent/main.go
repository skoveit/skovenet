package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/skoveit/skovenet/pkg/command"
	"github.com/skoveit/skovenet/pkg/discovery"
	"github.com/skoveit/skovenet/pkg/ipc"
	"github.com/skoveit/skovenet/pkg/logger"
	"github.com/skoveit/skovenet/pkg/node"
	"github.com/skoveit/skovenet/pkg/protocol"
)

var (
	debug = flag.Bool("debug", false, "Enable debug logging")
)

// RadarResult holds discovered node info
type RadarResult struct {
	PeerID    string `json:"peer_id"`
	Latency   int64  `json:"latency_ms"`
	Timestamp int64  `json:"timestamp"`
}

var (
	radarMu      sync.Mutex
	radarResults = make(map[string]RadarResult)
	radarActive  = false
	radarStart   time.Time
)

// Topology graph data
type TopoGraph struct {
	Nodes []TopoNode `json:"nodes"`
	Edges []TopoEdge `json:"edges"`
}
type TopoNode struct {
	ID string `json:"id"`
}
type TopoEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

var (
	topoMu     sync.Mutex
	topoGraph  = make(map[string][]string) // nodeID -> list of peers
	topoActive = false
)

func main() {
	flag.Parse()
	logger.SetDebug(*debug)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize node
	n, err := node.NewNode(ctx)
	if err != nil {
		logger.Fatalf("Failed to create node: %v", err)
	}

	// Setup protocol
	cmdHandler := command.NewHandler(n)
	proto := protocol.NewProtocol(n, cmdHandler)
	cmdHandler.SetProtocol(proto)
	n.SetProtocol(proto)

	// Handle radar pong responses
	proto.SetPongCallback(func(peerID, payload string) {
		radarMu.Lock()
		defer radarMu.Unlock()
		if radarActive {
			latency := time.Since(radarStart).Milliseconds()
			radarResults[peerID] = RadarResult{
				PeerID:    peerID,
				Latency:   latency,
				Timestamp: time.Now().Unix(),
			}
		}
	})

	// Handle topology responses
	proto.SetTopoCallback(func(peerID string, peers []string) {
		topoMu.Lock()
		defer topoMu.Unlock()
		if topoActive {
			topoGraph[peerID] = peers
		}
	})

	// Start discovery
	discovery.SuppressMDNSWarnings() // Suppress noisy Windows mDNS warnings
	disc := discovery.NewMDNSDiscovery(n)
	if err := disc.Start(); err != nil {
		logger.Fatalf("Failed to start discovery: %v", err)
	}

	// Start IPC server
	var server *ipc.AgentServer
	server, err = ipc.NewAgentServer(func(cmd string, args []string) string {
		return handleCommand(cmd, args, n, proto, server)
	})
	if err != nil {
		logger.Fatalf("Failed to start IPC: %v", err)
	}

	// Forward P2P responses to controller with command ID for correlation
	cmdHandler.SetResponseCallback(func(source, payload, cmdID string) {
		server.PushWithCmdID(payload, cmdID)
	})

	// Notify controller on peer changes
	n.PeerManager().SetCallback(func(peerID string, connected bool) {
		if connected {
			server.PushEvent("peer_connected", peerID)
		} else {
			server.PushEvent("peer_disconnected", peerID)
		}
	})

	logger.Debug("Node started: %s", n.ID().String())
	logger.Debug("Listening on: %s", n.Addrs())

	// Wait for shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	server.Stop()
	disc.Stop()
}

func handleCommand(cmd string, args []string, n *node.Node, proto *protocol.Protocol, _ *ipc.AgentServer) string {
	switch cmd {
	case "id":
		return n.ID().String()

	case "peers":
		return n.ListPeers()

	case "peerlist":
		// Return JSON list of peer IDs for tab completion
		peers := n.PeerManager().List()
		ids := make([]string, len(peers))
		for i, p := range peers {
			ids[i] = p.String()
		}
		data, _ := json.Marshal(ids)
		return string(data)

	case "radar":
		// Broadcast ping and collect responses
		radarMu.Lock()
		radarResults = make(map[string]RadarResult)
		radarActive = true
		radarStart = time.Now()
		radarMu.Unlock()

		// Send radar ping
		pingID := fmt.Sprintf("radar-%d", time.Now().UnixNano())
		proto.Broadcast(pingID)

		// Wait for responses (configurable timeout)
		timeout := 3 * time.Second
		if len(args) > 0 {
			if d, err := time.ParseDuration(args[0]); err == nil {
				timeout = d
			}
		}
		time.Sleep(timeout)

		// Collect results
		radarMu.Lock()
		radarActive = false
		results := make([]RadarResult, 0, len(radarResults))
		for _, r := range radarResults {
			results = append(results, r)
		}
		radarMu.Unlock()

		// Return JSON results
		data, _ := json.Marshal(results)
		return string(data)

	case "topology":
		// Broadcast topology request and collect peer lists
		topoMu.Lock()
		topoGraph = make(map[string][]string)
		topoActive = true
		topoMu.Unlock()

		// Add our own peer list
		myPeers := n.PeerManager().List()
		myPeerIDs := make([]string, len(myPeers))
		for i, p := range myPeers {
			myPeerIDs[i] = p.String()
		}
		topoMu.Lock()
		topoGraph[n.ID().String()] = myPeerIDs
		topoMu.Unlock()

		// Request topology from all nodes
		reqID := fmt.Sprintf("topo-%d", time.Now().UnixNano())
		proto.BroadcastTopology(reqID)

		// Wait for responses
		time.Sleep(3 * time.Second)

		// Build graph
		topoMu.Lock()
		topoActive = false

		nodeSet := make(map[string]bool)
		edges := []TopoEdge{}
		edgeSet := make(map[string]bool)

		for nodeID, peers := range topoGraph {
			nodeSet[nodeID] = true
			for _, peer := range peers {
				nodeSet[peer] = true
				// Create sorted edge key to avoid duplicates
				edgeKey := nodeID + "-" + peer
				if peer < nodeID {
					edgeKey = peer + "-" + nodeID
				}
				if !edgeSet[edgeKey] {
					edgeSet[edgeKey] = true
					edges = append(edges, TopoEdge{Source: nodeID, Target: peer})
				}
			}
		}

		nodes := make([]TopoNode, 0, len(nodeSet))
		for nodeID := range nodeSet {
			nodes = append(nodes, TopoNode{ID: nodeID})
		}

		graph := TopoGraph{Nodes: nodes, Edges: edges}
		topoMu.Unlock()

		data, _ := json.Marshal(graph)
		return string(data)

	case "sign":
		if len(args) < 1 {
			return "usage: sign <private_key_base64>"
		}
		proto.SetPrivateKey(args[0])
		return "signed"

	case "send":
		if len(args) < 2 {
			return "usage: send <nodeID> <command>"
		}
		if !proto.HasPrivateKey() {
			return "error: not signed in. Use 'sign <private_key>' first"
		}
		return proto.SendCommand(args[0], strings.Join(args[1:], " "))

	case "quit":
		return "goodbye"

	default:
		return fmt.Sprintf("unknown command: %s", cmd)
	}
}
