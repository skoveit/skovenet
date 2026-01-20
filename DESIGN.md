# DESIGN.md

## Project Overview

**SkoveNet** is a fully decentralized peer-to-peer (P2P) Command & Control (C2) system that eliminates the traditional single point of failure inherent in centralized C2 architectures. Unlike conventional C2 systems that rely on a central server or domain, SkoveNet operates as a self-healing mesh network where each agent (node) can communicate with any other agent through multi-hop routing.

### Core Philosophy

Traditional C2 systems are vulnerable to takedown because they depend on a central server. SkoveNet inverts this model: **there is no server, no domain, no single point of failure**. The operator is simply whoever possesses the cryptographic secret key, and can issue commands from any node in the network.

---

## Architecture


### System Components

#### 1. **Node** (`pkg/node/`)
The fundamental building block of the network. Each node represents a single agent in the mesh.

**Responsibilities:**
- Initialize libp2p host with Ed25519 identity
- Manage network connections and listeners
- Enforce peer connection limits (max 5 peers)
- Handle network events (connections/disconnections)
- Provide NAT traversal capabilities

**Key Features:**
- Uses libp2p for P2P networking
- Ed25519 cryptographic identity
- Automatic port mapping (UPnP/NAT-PMP)
- Dynamic port allocation (listens on random available port)

#### 2. **Peer Manager** (`pkg/node/peer_manager.go`)
Manages the set of connected peers for each node.

**Responsibilities:**
- Maintain a maximum of 5 peer connections
- Track connection timestamps
- Handle peer addition/removal
- Prevent connection overflow
- Thread-safe peer list operations

**Design Rationale:**
The 5-peer limit is intentional to:
- Minimize network traffic footprint
- Reduce detectability
- Maintain mesh connectivity without overwhelming individual nodes
- Enable efficient message routing

#### 3. **Protocol** (`pkg/protocol/`)
Implements the custom mesh communication protocol.

**Protocol ID:** `/mesh-c2/1.0.0`

**Message Types:**
- `command`: Execute a command on target node
- `response`: Return command execution results
- `route`: (Reserved for future routing enhancements)

**Message Structure:**
```json
{
  "type": "command|response|route",
  "id": "unique-message-id",
  "source": "source-peer-id",
  "target": "target-peer-id",
  "payload": "command or response data",
  "timestamp": 1234567890,
  "ttl": 10,
  "visited": ["peer-id-1", "peer-id-2"]
}
```

**Routing Algorithm:**
1. **Direct Delivery**: If target is a direct peer, send immediately
2. **Flood Routing**: Otherwise, forward to all connected peers (except visited)
3. **Loop Prevention**: Track visited nodes to prevent infinite loops
4. **TTL Mechanism**: Messages expire after 10 hops

#### 4. **Discovery** (`pkg/discovery/`)
Enables automatic peer discovery on local networks.

**Current Implementation:**
- mDNS (Multicast DNS) for local network discovery
- Service name: `_mesh-c2._tcp`
- Automatic peer connection on discovery (if not at max peers)

**Future Extensions:**
- DHT-based discovery for internet-wide networks
- Bootstrap node support
- Relay node discovery

#### 5. **Command Handler** (`pkg/command/`)
Processes incoming commands and generates responses.

**Workflow:**
1. Receive command message
2. Execute command via executor
3. Capture output/errors
4. Send response back to source node

**Executor:**
- Currently executes shell commands (via `os/exec`)
- Captures stdout/stderr
- Returns execution results

---

## Network Protocol

### Connection Lifecycle

```
┌─────────┐                                    ┌─────────┐
│ Node A  │                                    │ Node B  │
└────┬────┘                                    └────┬────┘
     │                                              │
     │  1. mDNS Discovery                           │
     │─────────────────────────────────────────────▶│
     │                                              │
     │  2. libp2p Connection Handshake              │
     │◀────────────────────────────────────────────▶│
     │                                              │
     │  3. Peer Manager: Add Peer (if < 5)          │
     │◀─────────────────────────────────────────────│
     │                                              │
     │  4. Protocol Stream Ready                    │
     │◀────────────────────────────────────────────▶│
     │                                              │
     │  5. Message Exchange                         │
     │◀────────────────────────────────────────────▶│
     │                                              │
```

### Message Flow

**Scenario: Node A sends command to Node E (not directly connected)**

```
A (source) → B → C → E (target)
             ↓
             D (also receives but doesn't forward to E since E is target)
```

**Step-by-step:**
1. A creates command message with target=E, TTL=10, visited=[A]
2. A sends to all peers (B, D)
3. B receives, adds self to visited [A,B], TTL=9, forwards to C
4. D receives, adds self to visited [A,D], TTL=9, forwards to peers
5. C receives from B, adds self to visited [A,B,C], TTL=8
6. C checks if E is target → YES
7. E executes command, sends response back to A via reverse routing

---

## Security Model

### Current Implementation

#### Cryptographic Identity
- Each node has an Ed25519 key pair
- Peer IDs are derived from public keys
- libp2p Noise protocol for transport encryption

#### Message Authenticity
- Messages are currently **not** cryptographically signed
- Trust is implicit within the mesh
- No operator authentication mechanism

### Planned Security Enhancements

> [!WARNING]
> **Current Security Limitations**
> 
> The current implementation lacks:
> - Command signing/verification
> - Operator authentication
> - Key rotation/revocation
> - Anti-spoofing mechanisms

#### Planned: Ed25519 Command Signing
```
Operator (has secret key) → Signs command → Agents verify signature
```

Each command will include:
- Signature: `Ed25519.sign(secretKey, command)`
- Public key: Embedded or distributed via secure channel
- Timestamp: Prevent replay attacks

#### Planned: Key Management
- Rotating keys with JWT-like tokens
- Certificate Authority (CA) for key distribution
- Revocation lists for compromised keys

---

## Data Flow

### Command Execution Flow

```
┌──────────────┐
│   Operator   │
│ (any node)   │
└──────┬───────┘
       │
       │ 1. Issue command via CLI
       ▼
┌──────────────────┐
│   Protocol       │
│ SendCommand()    │
└──────┬───────────┘
       │
       │ 2. Create Message
       │    - type: command
       │    - target: node-id
       │    - payload: "whoami"
       ▼
┌──────────────────┐
│  Routing Layer   │
│ routeMessage()   │
└──────┬───────────┘
       │
       │ 3. Flood to peers
       │    (skip visited)
       ▼
┌──────────────────┐
│  Target Node     │
│ HandleStream()   │
└──────┬───────────┘
       │
       │ 4. Check target == self
       ▼
┌──────────────────┐
│ Command Handler  │
│   Handle()       │
└──────┬───────────┘
       │
       │ 5. Execute command
       ▼
┌──────────────────┐
│   Executor       │
│  Execute()       │
└──────┬───────────┘
       │
       │ 6. Capture output
       ▼
┌──────────────────┐
│   Protocol       │
│ SendResponse()   │
└──────┬───────────┘
       │
       │ 7. Route response back
       │    to source
       ▼
┌──────────────────┐
│  Source Node     │
│  (Operator)      │
└──────────────────┘
```

---

## Scalability & Performance

### Current Characteristics

| Metric | Value | Notes |
|--------|-------|-------|
| Max peers per node | 5 | Hardcoded in `node.go` |
| Message TTL | 10 hops | Prevents infinite routing |
| Stream timeout | 5 seconds | Per message send |
| Routing strategy | Flood | Simple but inefficient at scale |

### Known Limitations

#### 1. **Latency at Scale**
- **Problem**: Flood routing causes exponential message duplication
- **Impact**: 5-10 second delays at 1000+ nodes
- **Current**: Acceptable under 500 nodes (~2-5s)
- **Planned**: Epidemic routing or priority-based gossip

#### 2. **Network Partitioning**
- **Problem**: 5-peer limit can fragment network
- **Mitigation**: Needs intelligent peer selection
- **Planned**: DHT-based routing for guaranteed connectivity

#### 3. **NAT Traversal**
- **Problem**: Corporate firewalls block P2P connections
- **Current**: Requires manual relay/bootstrap nodes
- **Planned**: STUN/TURN integration, DoH tunneling

---

## Technology Stack

### Core Dependencies

- **libp2p** (`github.com/libp2p/go-libp2p`): P2P networking framework
  - Provides: Transport layer, NAT traversal, peer discovery, stream multiplexing
  - Version: v0.45.0

- **Noise Protocol**: Encrypted transport
  - Provides: Forward secrecy, authentication

- **WebRTC**: NAT hole punching
  - Provides: Direct connections through NATs

- **mDNS**: Local network discovery
  - Provides: Zero-config peer discovery on LANs

### Transport Protocols

Currently supported:
- TCP
- WebSocket (via libp2p)
- WebRTC (for NAT traversal)

Planned:
- DNS tunneling
- ICMP tunneling
- HTTP/HTTPS mimicry

---

## Deployment Model

### Single Binary
- Cross-platform: Windows, Linux, macOS, ARM
- Zero external dependencies
- Statically linked Go binary

### Execution Modes

#### Agent Mode (Default)
```bash
./skovenet
```
- Starts P2P node
- Joins mesh network
- Listens for commands
- Provides CLI for sending commands

#### Future: Operator Mode
```bash
./skovenet --operator --key secret.key
```
- Load operator secret key
- Sign all commands
- Broadcast to mesh

---

## Configuration

### Current Configuration
All configuration is currently hardcoded:

```go
const MaxPeers = 5
const ProtocolID = "/mesh-c2/1.0.0"
const MessageTTL = 10
```

### Planned Configuration
Environment variables or config file:
```yaml
network:
  max_peers: 5
  protocol_id: "/mesh-c2/1.0.0"
  listen_addr: "/ip4/0.0.0.0/tcp/0"

discovery:
  mdns_enabled: true
  dht_enabled: false
  bootstrap_peers:
    - /ip4/1.2.3.4/tcp/4001/p2p/QmBootstrap...

security:
  operator_pubkey: "ed25519:..."
  verify_commands: true

routing:
  ttl: 10
  strategy: "flood" # or "epidemic", "dht"
```

---

## Testing Strategy

### Unit Tests
- Protocol message marshaling/unmarshaling
- Peer manager operations
- Command execution

### Integration Tests
- Multi-node mesh formation
- Message routing across hops
- Peer discovery and connection

### Functional Tests
- End-to-end command execution
- Network resilience (node failures)
- NAT traversal scenarios

---

## Future Enhancements

### Short-term (MVP+)
1. **Command Signing**: Ed25519 signature verification
2. **Better Routing**: Replace flood with epidemic broadcast
3. **Configuration**: External config file support
4. **Logging**: Structured logging with levels

### Medium-term
1. **DHT Integration**: Kademlia DHT for peer discovery
2. **Relay Nodes**: Designated relay nodes for NAT traversal
3. **Traffic Obfuscation**: WebSocket/DNS/ICMP transports
4. **Key Rotation**: JWT-like token system

### Long-term
1. **GossipSub**: Replace flood routing with libp2p GossipSub
2. **Persistent Storage**: SQLite for message history
3. **Web UI**: Browser-based operator interface
4. **Multi-operator**: Multiple operators with different privilege levels

---

## Development Guidelines

### Code Organization

```
skovenet/
├── cmd/
│   └── agent/          # Main entry point
│       └── main.go
├── pkg/
│   ├── node/           # Core P2P node
│   │   ├── node.go
│   │   └── peer_manager.go
│   ├── protocol/       # Messaging protocol
│   │   ├── protocol.go
│   │   └── message.go
│   ├── discovery/      # Peer discovery
│   │   ├── discovery.go
│   │   └── mdns.go
│   └── command/        # Command handling
│       ├── handler.go
│       └── executor.go
└── static/             # Assets (diagrams, etc.)
```

### Coding Standards
- Use Go 1.25.4+
- Follow standard Go formatting (`gofmt`)
- Minimize external dependencies
- Prefer stdlib where possible
- Thread-safe concurrent code (use mutexes)

### Error Handling
- Log errors but don't crash the mesh
- Graceful degradation on peer failures
- Timeout all network operations

---

## Operational Considerations

### Monitoring
- Log peer connections/disconnections
- Track message routing paths
- Monitor TTL exhaustion
- Measure command latency

### Debugging
- Each node logs its peer ID (first 16 chars)
- Message routing shows hop path
- CLI commands for network inspection:
  - `peers`: List connected peers
  - `id`: Show node ID

### Resilience
- Automatic peer reconnection
- Self-healing mesh (new peers replace failed ones)
- No single point of failure
- Graceful handling of network partitions

---

## Threat Model

### Assumptions
- Attacker can monitor network traffic
- Attacker can block specific IPs/domains
- Attacker cannot break Ed25519 cryptography
- Attacker may compromise individual nodes

### Defenses
- **No central server**: Cannot be taken down by blocking one IP
- **Encrypted transport**: Noise protocol prevents eavesdropping
- **Mesh topology**: Network survives node failures
- **Planned: Command signing**: Prevents command injection

### Attack Vectors
- **Traffic analysis**: Pattern-based detection of P2P traffic
  - Mitigation: Pluggable transports (DNS, ICMP, HTTPS)
- **Sybil attack**: Attacker floods network with malicious nodes
  - Mitigation: Peer reputation, proof-of-work
- **Eclipse attack**: Isolate node by controlling all its peers
  - Mitigation: Diverse peer selection, DHT routing

---

## Glossary

- **Agent**: A single node in the mesh network
- **Operator**: Entity with the secret key to issue commands
- **Mesh**: Decentralized network topology where nodes connect to multiple peers
- **TTL**: Time-to-live, number of hops before message expires
- **Flood Routing**: Broadcasting messages to all peers
- **libp2p**: Modular P2P networking stack
- **mDNS**: Multicast DNS for local network discovery
- **NAT**: Network Address Translation, common in corporate/home networks
- **Noise Protocol**: Cryptographic framework for secure transport

---

## References

- [libp2p Documentation](https://docs.libp2p.io/)
- [Noise Protocol Framework](https://noiseprotocol.org/)
- [GossipSub Specification](https://github.com/libp2p/specs/tree/master/pubsub/gossipsub)
- [Kademlia DHT](https://pdos.csail.mit.edu/~petar/papers/maymounkov-kademlia-lncs.pdf)
