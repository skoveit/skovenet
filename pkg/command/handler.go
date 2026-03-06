package command

import (
	"context"

	"github.com/skoveit/skovenet/pkg/logger"
	"github.com/skoveit/skovenet/pkg/node"
	"github.com/skoveit/skovenet/pkg/protocol"
)

// ResponseCallback is called when a response is ready to be sent.
// Parameters: source peer ID, payload, originating command ID
type ResponseCallback func(source, payload, cmdID string)

type Handler struct {
	node             *node.Node
	executor         *Executor
	protocol         *protocol.Protocol
	responseCallback ResponseCallback
}

func NewHandler(n *node.Node) *Handler {
	return &Handler{
		node:     n,
		executor: NewExecutor(),
	}
}

func (h *Handler) SetProtocol(p *protocol.Protocol) {
	h.protocol = p
}

// SetResponseCallback sets a callback for when responses are received
func (h *Handler) SetResponseCallback(cb ResponseCallback) {
	h.responseCallback = cb
}

func (h *Handler) Handle(msg *protocol.Message) error {
	if msg.Type == protocol.MsgTypeResponse {
		logger.Debug("✓ Response from: %s (cmd: %s)", msg.Source, msg.CmdID)
		// Forward response to callback (for controller) with command ID
		if h.responseCallback != nil {
			h.responseCallback(msg.Source, msg.Payload, msg.CmdID)
		}
		return nil
	}

	logger.Debug("⚡ Executing: %s", msg.Payload)

	output, err := h.executor.Execute(context.Background(), msg.Payload)
	if err != nil {
		logger.Debug("❌ Error: %v", err)
		return err
	}

	if h.protocol != nil {
		// Send response with the originating command's ID for correlation
		h.protocol.SendResponseWithCmdID(msg.Source, output, msg.ID)
	}
	return nil
}
