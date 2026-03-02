package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// ---------------------------------------------------------------------------
// randStr
// ---------------------------------------------------------------------------

func TestRandStr_Length(t *testing.T) {
	for _, n := range []int{0, 1, 6, 20} {
		s := randStr(n)
		if len(s) != n {
			t.Errorf("randStr(%d) returned length %d", n, len(s))
		}
	}
}

func TestRandStr_ActuallyRandom(t *testing.T) {
	// The old bug always produced "aaaaaa". Generate several strings and
	// verify they are not all identical.
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		seen[randStr(6)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("randStr produced the same string 20 times: %v", seen)
	}
}

func TestRandStr_ValidChars(t *testing.T) {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	s := randStr(100)
	for _, c := range s {
		found := false
		for _, valid := range chars {
			if c == valid {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("randStr produced invalid character: %c", c)
		}
	}
}

// ---------------------------------------------------------------------------
// generateID
// ---------------------------------------------------------------------------

func TestGenerateID_Unique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateID()
		if ids[id] {
			t.Fatalf("generateID collision: %s", id)
		}
		ids[id] = true
	}
}

func TestGenerateID_Format(t *testing.T) {
	id := generateID()
	// Should be timestamp (14 chars) + random suffix (6 chars) = 20 chars
	if len(id) != 20 {
		t.Errorf("expected generateID length 20, got %d: %q", len(id), id)
	}
}

// ---------------------------------------------------------------------------
// NewMessage
// ---------------------------------------------------------------------------

func TestNewMessage_Fields(t *testing.T) {
	msg := NewMessage(MsgTypeCommand, "src", "tgt", "whoami")

	if msg.Type != MsgTypeCommand {
		t.Errorf("Type = %v, want command", msg.Type)
	}
	if msg.Source != "src" {
		t.Errorf("Source = %v, want src", msg.Source)
	}
	if msg.Target != "tgt" {
		t.Errorf("Target = %v, want tgt", msg.Target)
	}
	if msg.Payload != "whoami" {
		t.Errorf("Payload = %v, want whoami", msg.Payload)
	}
	if msg.TTL != 10 {
		t.Errorf("TTL = %d, want 10", msg.TTL)
	}
	if len(msg.Visited) != 1 || msg.Visited[0] != "src" {
		t.Errorf("Visited = %v, want [src]", msg.Visited)
	}
	if msg.ID == "" {
		t.Error("ID should not be empty")
	}
	if msg.CmdID != "" {
		t.Errorf("CmdID should be empty for new message, got %q", msg.CmdID)
	}
}

func TestNewMessage_TimestampRecent(t *testing.T) {
	before := time.Now().Unix()
	msg := NewMessage(MsgTypePing, "a", "b", "")
	after := time.Now().Unix()

	if msg.Timestamp < before || msg.Timestamp > after {
		t.Errorf("Timestamp %d not between %d and %d", msg.Timestamp, before, after)
	}
}

// ---------------------------------------------------------------------------
// Marshal / Unmarshal roundtrip
// ---------------------------------------------------------------------------

func TestMarshalUnmarshal_Roundtrip(t *testing.T) {
	original := NewMessage(MsgTypeCommand, "src123", "tgt456", "ls -la")
	original.CmdID = "cmd-abc"

	data, err := original.Marshal()
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	decoded, err := UnmarshalMessage(data)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type mismatch: %v != %v", decoded.Type, original.Type)
	}
	if decoded.ID != original.ID {
		t.Errorf("ID mismatch")
	}
	if decoded.CmdID != original.CmdID {
		t.Errorf("CmdID mismatch: %q != %q", decoded.CmdID, original.CmdID)
	}
	if decoded.Source != original.Source {
		t.Errorf("Source mismatch")
	}
	if decoded.Target != original.Target {
		t.Errorf("Target mismatch")
	}
	if decoded.Payload != original.Payload {
		t.Errorf("Payload mismatch")
	}
	if decoded.TTL != original.TTL {
		t.Errorf("TTL mismatch")
	}
	if decoded.Timestamp != original.Timestamp {
		t.Errorf("Timestamp mismatch")
	}
}

func TestMarshalUnmarshal_CmdID_OmitEmpty(t *testing.T) {
	msg := NewMessage(MsgTypePong, "a", "b", "")
	data, _ := msg.Marshal()

	// CmdID should be omitted from JSON when empty
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if _, exists := raw["cmd_id"]; exists {
		t.Error("cmd_id should be omitted when empty")
	}
}

func TestUnmarshalMessage_InvalidJSON(t *testing.T) {
	_, err := UnmarshalMessage([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ---------------------------------------------------------------------------
// Visited tracking
// ---------------------------------------------------------------------------

func TestAddVisited(t *testing.T) {
	msg := NewMessage(MsgTypeCommand, "src", "tgt", "cmd")
	initial := len(msg.Visited)
	initialTTL := msg.TTL

	// Use a fake peer ID (real peer IDs are multihash-encoded)
	fakeID := peer.ID("fake-node-1")
	msg.AddVisited(fakeID)

	if len(msg.Visited) != initial+1 {
		t.Errorf("Visited length = %d, want %d", len(msg.Visited), initial+1)
	}
	if msg.TTL != initialTTL-1 {
		t.Errorf("TTL = %d, want %d", msg.TTL, initialTTL-1)
	}
}

func TestHasVisited(t *testing.T) {
	msg := NewMessage(MsgTypeCommand, "src", "tgt", "cmd")
	fakeID := peer.ID("fake-node-1")

	if msg.HasVisited(fakeID) {
		t.Error("should not have visited fake-node-1 yet")
	}

	msg.AddVisited(fakeID)

	if !msg.HasVisited(fakeID) {
		t.Error("should have visited fake-node-1 after AddVisited")
	}
}

// ---------------------------------------------------------------------------
// MessageType.String
// ---------------------------------------------------------------------------

func TestMessageType_String(t *testing.T) {
	tests := []struct {
		mt   MessageType
		want string
	}{
		{MsgTypeCommand, "command"},
		{MsgTypeResponse, "response"},
		{MsgTypePing, "ping"},
		{MsgTypePong, "pong"},
		{MsgTypeTopoReq, "toporeq"},
		{MsgTypeTopoRes, "topores"},
		{MsgTypeRoute, "route"},
	}
	for _, tt := range tests {
		if got := tt.mt.String(); got != tt.want {
			t.Errorf("MessageType(%q).String() = %q, want %q", tt.mt, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// SignableContent
// ---------------------------------------------------------------------------

func TestSignableContent_Deterministic(t *testing.T) {
	msg := NewMessage(MsgTypeCommand, "src", "tgt", "whoami")
	c1 := msg.SignableContent()
	c2 := msg.SignableContent()

	if string(c1) != string(c2) {
		t.Error("SignableContent should be deterministic")
	}
}

func TestSignableContent_ExcludesVisitedAndSignature(t *testing.T) {
	msg := NewMessage(MsgTypeCommand, "src", "tgt", "whoami")
	before := string(msg.SignableContent())

	msg.AddVisited(peer.ID("hop1"))
	msg.Signature = "fakesig"
	after := string(msg.SignableContent())

	if before != after {
		t.Error("SignableContent should not change when Visited or Signature changes")
	}
}

// ---------------------------------------------------------------------------
// IsSigned
// ---------------------------------------------------------------------------

func TestIsSigned(t *testing.T) {
	msg := NewMessage(MsgTypeCommand, "src", "tgt", "cmd")
	if msg.IsSigned() {
		t.Error("new message should not be signed")
	}
	msg.Signature = "something"
	if !msg.IsSigned() {
		t.Error("message with Signature should be signed")
	}
}
