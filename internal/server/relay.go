package server

import (
	"encoding/json"
	"log"
	"net/http"

	"xfer/internal/protocol"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  64 * 1024, // 64KB
	WriteBufferSize: 64 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for now (CORS will be handled in middleware)
		return true
	},
}

// handleWebSocketConnection upgrades and handles WebSocket connections
func (s *Server) handleWebSocketConnection(w http.ResponseWriter, r *http.Request, session *Session) {
	// Upgrade connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Determine if this is sender or receiver based on session type
	if session.Type == protocol.SessionTypeSend {
		s.handleSenderConnection(conn, session)
	} else {
		s.handleReceiverConnection(conn, session)
	}
}

// handleSenderConnection handles the sender's WebSocket connection
func (s *Server) handleSenderConnection(conn *websocket.Conn, session *Session) {
	defer conn.Close()

	// Set sender connection
	session.SetSenderConn(conn)
	defer func() {
		session.SetSenderConn(nil)
	}()

	log.Printf("Sender connected to session %s", session.Token)

	// Send status: waiting for receiver
	s.sendStatus(conn, protocol.StatePending, "Waiting for receiver to connect...")

	// Wait for receiver to connect, then relay messages
	for {
		// Check if session is expired or failed
		if session.IsExpired() || session.GetState().IsTerminal() {
			break
		}

		// Read message from sender
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Sender read error: %v", err)
			session.SetState(protocol.StateFailed)
			break
		}

		// Only handle binary and text messages
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}

		// Check if receiver is connected
		session.mu.Lock()
		recvConn := session.RecvConn
		session.mu.Unlock()

		if recvConn == nil {
			// Receiver not connected yet, keep waiting
			continue
		}

		// Update state to active if this is the first message
		if session.GetState() == protocol.StatePending {
			session.SetState(protocol.StateActive)
			s.sendStatus(conn, protocol.StateActive, "Transfer in progress...")
		}

		// Relay message to receiver
		session.mu.Lock()
		if session.RecvConn != nil {
			err = session.RecvConn.WriteMessage(messageType, data)
		}
		session.mu.Unlock()

		if err != nil {
			log.Printf("Failed to relay to receiver: %v", err)
			session.SetState(protocol.StateFailed)
			break
		}
	}

	log.Printf("Sender disconnected from session %s", session.Token)
}

// handleReceiverConnection handles the receiver's WebSocket connection
func (s *Server) handleReceiverConnection(conn *websocket.Conn, session *Session) {
	defer conn.Close()

	// Set receiver connection
	session.SetRecvConn(conn)
	defer func() {
		session.SetRecvConn(nil)
	}()

	log.Printf("Receiver connected to session %s", session.Token)

	// Check if sender is connected
	session.mu.Lock()
	hasSender := session.SenderConn != nil
	session.mu.Unlock()

	if hasSender {
		session.SetState(protocol.StateActive)
		s.sendStatus(conn, protocol.StateActive, "Connected. Transfer starting...")
	} else {
		s.sendStatus(conn, protocol.StatePending, "Waiting for sender...")
	}

	// Relay messages from sender to receiver
	for {
		// Check if session is expired or failed
		if session.IsExpired() || session.GetState().IsTerminal() {
			break
		}

		// Read message from receiver (could be acknowledgments or upload data)
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Receiver read error: %v", err)
			session.SetState(protocol.StateFailed)
			break
		}

		// Only handle binary and text messages
		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}

		// For receive sessions, relay back to sender (upload scenario)
		session.mu.Lock()
		senderConn := session.SenderConn
		session.mu.Unlock()

		if senderConn != nil {
			session.mu.Lock()
			err = session.SenderConn.WriteMessage(messageType, data)
			session.mu.Unlock()

			if err != nil {
				log.Printf("Failed to relay to sender: %v", err)
				session.SetState(protocol.StateFailed)
				break
			}
		}
	}

	log.Printf("Receiver disconnected from session %s", session.Token)
}

// sendStatus sends a status message over WebSocket
func (s *Server) sendStatus(conn *websocket.Conn, state protocol.SessionState, message string) {
	status := protocol.TransferStatus{
		State:   state,
		Message: message,
	}

	msg := protocol.Message{
		Type:    protocol.MessageTypeStatus,
		Payload: status,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal status: %v", err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		log.Printf("Failed to send status: %v", err)
	}
}
