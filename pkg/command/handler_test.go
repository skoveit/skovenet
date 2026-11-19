package command

import (
	"testing"

	"github.com/skoveit/skovenet/pkg/protocol"
)

func TestHandler_RejectsNonCommandExecution(t *testing.T) {
	tests := []struct {
		name    string
		msgType protocol.MessageType
	}{
		{"route", protocol.MsgTypeRoute},
		{"ping", protocol.MsgTypePing},
		{"pong", protocol.MsgTypePong},
		{"topology request", protocol.MsgTypeTopoReq},
		{"topology response", protocol.MsgTypeTopoRes},
		{"unknown", protocol.MessageType("unknown")},
		{"empty", protocol.MessageType("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &mockCommand{name: "sentinel"}
			executor := NewExecutor()
			executor.Register(cmd)
			handler := &Handler{executor: executor}
			responseCalled := false
			handler.SetResponseCallback(func(source, payload, cmdID string) {
				responseCalled = true
			})

			err := handler.Handle(&protocol.Message{
				Type:    tt.msgType,
				Payload: "sentinel unexpected-execution",
			})
			if err == nil {
				t.Error("expected an error for an unsupported message type")
			}
			if cmd.called {
				t.Error("non-command message executed its payload")
			}
			if responseCalled {
				t.Error("non-response message reached the response callback")
			}
		})
	}
}

func TestHandler_DispatchesCommand(t *testing.T) {
	cmd := &mockCommand{name: "sentinel"}
	executor := NewExecutor()
	executor.Register(cmd)
	handler := &Handler{executor: executor}

	// The protocol verifies command signatures before calling the handler.
	err := handler.Handle(&protocol.Message{
		Type:    protocol.MsgTypeCommand,
		Source:  "operator",
		Payload: "sentinel expected-arguments",
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !cmd.called || cmd.lastArgs != "expected-arguments" {
		t.Fatalf("command dispatch: called=%v, args=%q", cmd.called, cmd.lastArgs)
	}
}

func TestHandler_ResponseDoesNotExecutePayload(t *testing.T) {
	cmd := &mockCommand{name: "sentinel"}
	executor := NewExecutor()
	executor.Register(cmd)
	handler := &Handler{executor: executor}
	msg := &protocol.Message{
		Type:    protocol.MsgTypeResponse,
		Source:  "remote-peer",
		Payload: "sentinel response-output",
		CmdID:   "original-command",
	}
	callbackCalls := 0
	handler.SetResponseCallback(func(source, payload, cmdID string) {
		callbackCalls++
		if source != msg.Source || payload != msg.Payload || cmdID != msg.CmdID {
			t.Errorf("response callback received (%q, %q, %q)", source, payload, cmdID)
		}
	})

	if err := handler.Handle(msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if cmd.called {
		t.Error("response message executed its payload")
	}
	if callbackCalls != 1 {
		t.Errorf("response callback called %d times, want 1", callbackCalls)
	}
}
