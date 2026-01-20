package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/skoveit/skovenet/pkg/signing"

	"github.com/libp2p/go-libp2p/core/peer"
)

type MessageType string

const (
	MsgTypeCommand  MessageType = "command"
	MsgTypeResponse MessageType = "response"
	MsgTypeRoute    MessageType = "route"
	MsgTypePing     MessageType = "ping"    // Radar discovery ping
	MsgTypePong     MessageType = "pong"    // Radar discovery response
	MsgTypeTopoReq  MessageType = "toporeq" // Topology request
	MsgTypeTopoRes  MessageType = "topores" // Topology response (peer list)
)

// String returns the human-readable string representation of the message type
func (mt MessageType) String() string {
	return string(mt)
}

type Message struct {
	Type      MessageType `json:"type"`
	ID        string      `json:"id"`
	Source    string      `json:"source"`
	Target    string      `json:"target"`
	Payload   string      `json:"payload"`
	Timestamp int64       `json:"timestamp"`
	TTL       int         `json:"ttl"`
	Visited   []string    `json:"visited"`
	Signature string      `json:"signature,omitempty"`
}

func NewMessage(msgType MessageType, source, target, payload string) *Message {
	return &Message{
		Type:      msgType,
		ID:        generateID(),
		Source:    source,
		Target:    target,
		Payload:   payload,
		Timestamp: time.Now().Unix(),
		TTL:       10,
		Visited:   []string{source},
	}
}

func (m *Message) AddVisited(nodeID peer.ID) {
	m.Visited = append(m.Visited, nodeID.String())
	m.TTL--
}

func (m *Message) HasVisited(nodeID peer.ID) bool {
	id := nodeID.String()
	for _, v := range m.Visited {
		if v == id {
			return true
		}
	}
	return false
}

func (m *Message) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

func UnmarshalMessage(data []byte) (*Message, error) {
	var msg Message
	err := json.Unmarshal(data, &msg)
	return &msg, err
}

// SignableContent returns the bytes to be signed.
// This excludes Visited and Signature fields to ensure signature remains valid
// as the message is routed through the network.
func (m *Message) SignableContent() []byte {
	// Create deterministic content: type|source|target|payload|timestamp
	content := string(m.Type) + "|" + m.Source + "|" + m.Target + "|" + m.Payload + "|" + fmt.Sprintf("%d", m.Timestamp)
	return []byte(content)
}

// SignWithKey signs the message with the given private key.
// The privateKeyB64 should be a base64-encoded Ed25519 private key.
func (m *Message) SignWithKey(privateKeyB64 string) error {
	sig, err := signing.SignWithKey(m.SignableContent(), privateKeyB64)
	if err != nil {
		return err
	}
	m.Signature = sig
	return nil
}

// VerifySignature verifies the message signature.
func (m *Message) VerifySignature() bool {
	if m.Signature == "" {
		return false
	}
	return signing.Verify(m.SignableContent(), m.Signature)
}

// IsSigned returns true if the message has a signature.
func (m *Message) IsSigned() bool {
	return m.Signature != ""
}

func generateID() string {
	return time.Now().Format("20060102150405") + randStr(6)
}

func randStr(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}
