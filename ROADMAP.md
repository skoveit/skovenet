## Phase 1: Security 
*Focus: Closing critical security gaps and protecting operator keys.*

- **Secure Command Signing**: Move all signing logic to the controller; the agent should never see the private key.
- **Key Management**: Support for loading operator public keys from config/flags instead of hardcoding.
- **Multi-Operator Support**: Allow multiple trusted public keys in the agent.
- **Message De-duplication**: Implement a time-bounded LRU cache for message IDs to prevent replay/loops.

---

## Phase 2: WAN Enablement
*Focus: Moving beyond mDNS and enabling internet-wide deployments.*

- **Manual Peering**: Implement `connect <multiaddr>` command in the controller.
- **Identity Persistence**: Save/load node PeerID and Private Key to disk so identities survive restarts.
- **Bootstrap Nodes**: Support for a static list of bootstrap peers to join the mesh from anywhere.
- **NAT Traversal**: Enable AutoRelay and Circuit Relay v2 for nodes behind restrictive firewalls.
- **DHT Discovery**: Integrate Kademlia DHT for decentralized peer discovery on the internet.

---

## Phase 3: Core C2 Capabilities
*Focus: Adding the essential tools every operator needs.*

- **File Transfer**: `upload` and `download` commands with chunking and integrity checks.
- **Persistent Persistence**: Automated installers for Linux (systemd) and Windows (Registry/Tasks).
- **Interactive Shell**: Optimization for low-latency command execution.
- **Process Management**: Commands to list, kill, and spawn processes with resource tracking.
- **SOCKS Proxy**: Peer-to-peer proxying through the mesh.

---

## Phase 4: Stealth & Evasion
*Focus: Staying under the radar.*

- **Traffic Obfuscation**: Support for WebSocket, DNS, or ICMP transports to bypass DPI.
- **Jitter & Sleep**: Implement malleable timing for inter-node communication.
- **Custom Handshakes**: Encrypt initial libp2p handshakes with rotating shared secrets.
- **Memory-Only Operation**: Logic for reflective loading or minimal disk footprint.

---

## Phase 5: Ecosystem & UX
*Focus: Making SkoveNet easier to manage at scale.*

- **Headless Controller**: A detached server for 24/7 mesh monitoring.
- **API Layer**: REST/gRPC API for integrating with external GUIs or SOAR platforms.
- **Web Dashboard**: Real-time graph visualization of the mesh topology.
- **Automated Deployment**: Ansible/Terraform scripts for rapid mesh standup.

---

> **Want to contribute?** Check out the [GitHub Issues](https://github.com/skoveit/skovenet/issues)
