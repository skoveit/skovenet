Most C2 frameworks today rely on a Client-Server architecture. This creates a critical Single Point of Failure: if the Teamserver IP or Redirector domain is burned, the entire fleet of agents is lost.

**SkoveNet** shifts the paradigm to a Gossip-based (Not-Full)Mesh Network. There is no central server. Every agent is a peer, and commands propagate through the network via cryptographically signed gossip.


![structure diagram](static/structure_diagram.png)
**No server. No domain. No single point of failure.**

## Architectural Comparison

| Vector                         | Traditional C2 | SkoveNet                              |
|--------------------------------|----------------|--------------------------------------|
| Central server?                | Yes            | Never                                |
| Killable by blocking 1 IP?     | Yes            | Impossible                           |
| Operator has fixed location?   | Yes            | No – any node with the secret key    |
| Network dies when nodes drop?  | Yes            | No – self-healing graph              |
| Detectable by traffic pattern? | Easy           | Extremely hard (only 5 neighbors)    |
| Command authenticity           | Server cert    | Ed25519-signed by secret key         |



## Core Features

- Fully decentralized P2P graph
- Max 5 neighbors per agent → tiny traffic footprint
- Automatic self-healing
- Operator = whoever has the secret key
- Commands signed with Ed25519 → no spoofing
- End-to-end encrypted
- GossipSub broadcast (fast & reliable)
- Single binary, zero dependencies – works on Windows, Linux, macOS, ARM
- NAT traversal & hole punching built-in




## 🚧 Current Engineering Challenges & Limitations

While **SkoveNet** achieves exceptional resilience by eliminating the traditional Single Point of Failure (SPoF), fully distributed systems come with their own dragons to tame. These are the major engineering fronts still under active development.


| Area | Summary of Problem | Current Status | Planned Improvement |
|------|--------------------|----------------|----------------------|
| 🌐 NAT Traversal & Egress | Corporate NAT/Firewalls block P2P connections; hole punching fails on Symmetric NATs. | Requires manual Bootstrap/Relay nodes. | Add STUN/TURN/DoH to improve NAT traversal and reduce relay dependency. |
| ⚡ Latency & Scalability | GossipSub slows down in large meshes (5–10s delays at 1000+ nodes). | Stable performance (~2–5s) under 500 nodes. | Add epidemic routing or priority-based gossip for <2s delivery. |
| 🔑 Key Management | Static Ed25519 key; no rotation or revocation if compromised. | Fixed keypair, loss means full loss of control. | Implement JWT-like tokens or rotating CA for secure revocation. |
| 🕵️ Traffic Evasion | Raw libp2p traffic detected by NIDS; suspicious high-port encrypted patterns. | Using default Noise protocol transport. | Wrap traffic in WebSockets, DNS, ICMP, or other pluggable transports. |
