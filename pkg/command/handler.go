package command

import (
	"github.com/skoveit/skovenet/pkg/logger"
	"github.com/skoveit/skovenet/pkg/node"
	"github.com/skoveit/skovenet/pkg/protocol"
)

// ResponseCallback is called when a response is ready to be sent
type ResponseCallback func(source, payload string)

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
		logger.Debug("✓ Response from: %s", msg.Source)
		// Forward response to callback (for controller)
		if h.responseCallback != nil {
			h.responseCallback(msg.Source, msg.Payload)
		}
		return nil
	}

	logger.Debug("⚡ Executing: %s", msg.Payload)

	output, err := h.executor.Execute(msg.Payload)
	if err != nil {
		logger.Debug("❌ Error: %v", err)
		return err
	}

	if h.protocol != nil {
		h.protocol.SendResponse(msg.Source, output)
	}
	return nil
}
