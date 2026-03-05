package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"xfer/internal/protocol"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  64 * 1024,
	WriteBufferSize: 64 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func closeConnGracefully(conn *websocket.Conn) {
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	conn.Close()
}

// handleWebSocketConnection upgrades and handles WebSocket connections
func (s *Server) handleWebSocketConnection(w http.ResponseWriter, r *http.Request, session *Session) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	// Determine if this is CLI (first connection) or browser (second connection)
	session.mu.Lock()
	isFirstConnection := session.SenderConn == nil && session.RecvConn == nil
	session.mu.Unlock()

	if session.Type == protocol.SessionTypeSend {
		if isFirstConnection {
			s.handleCLISender(conn, session)
		} else {
			s.handleBrowserReceiver(conn, session)
		}
	} else {
		if isFirstConnection {
			s.handleCLIReceiver(conn, session)
		} else {
			s.handleBrowserSender(conn, session)
		}
	}
}

// handleCLISender handles CLI connection for send sessions
func (s *Server) handleCLISender(conn *websocket.Conn, session *Session) {
	defer closeConnGracefully(conn)

	session.SetSenderConn(conn)
	defer session.SetSenderConn(nil)

	log.Printf("CLI sender connected to session %s", session.Token)

	s.sendStatus(conn, protocol.StatePending, "Waiting for receiver to scan QR code...")

	dataChan := make(chan wsMessage, 100)
	doneChan := make(chan struct{})
	browserReady := make(chan struct{})

	session.mu.Lock()
	session.dataChan = dataChan
	session.doneChan = doneChan
	session.browserReady = browserReady
	session.mu.Unlock()

	<-browserReady

	session.SetState(protocol.StateActive)
	s.sendStatus(conn, protocol.StateActive, "Receiver connected, starting transfer...")

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("CLI sender read error: %v", err)
			break
		}

		if messageType == websocket.TextMessage {
			var msg protocol.Message
			if json.Unmarshal(data, &msg) == nil && msg.Type == protocol.MessageTypeComplete {
				log.Printf("CLI sender completed for session %s", session.Token)
				break
			}
		}

		select {
		case dataChan <- wsMessage{Type: messageType, Data: data}:
		case <-doneChan:
			return
		}
	}

	close(dataChan)

	<-doneChan
	log.Printf("CLI sender disconnected from session %s", session.Token)
}

// handleBrowserReceiver handles browser connection for download
func (s *Server) handleBrowserReceiver(conn *websocket.Conn, session *Session) {
	defer closeConnGracefully(conn)

	session.SetRecvConn(conn)
	defer session.SetRecvConn(nil)

	log.Printf("Browser receiver connected to session %s", session.Token)

	session.mu.Lock()
	dataChan := session.dataChan
	doneChan := session.doneChan
	browserReady := session.browserReady
	session.mu.Unlock()

	if browserReady != nil {
		close(browserReady)
	}

	if dataChan != nil {
		for msg := range dataChan {
			if err := conn.WriteMessage(msg.Type, msg.Data); err != nil {
				log.Printf("Browser receiver write error: %v", err)
				break
			}
		}
	}

	if doneChan != nil {
		close(doneChan)
	}

	session.SetState(protocol.StateComplete)
	log.Printf("Browser receiver completed for session %s", session.Token)
}

// handleCLIReceiver handles CLI connection for receive sessions
func (s *Server) handleCLIReceiver(conn *websocket.Conn, session *Session) {
	defer closeConnGracefully(conn)

	session.SetRecvConn(conn)
	defer session.SetRecvConn(nil)

	log.Printf("CLI receiver connected to session %s", session.Token)

	s.sendStatus(conn, protocol.StatePending, "Waiting for sender to scan QR code...")

	dataChan := make(chan wsMessage, 100)
	doneChan := make(chan struct{})
	browserReady := make(chan struct{})

	session.mu.Lock()
	session.dataChan = dataChan
	session.doneChan = doneChan
	session.browserReady = browserReady
	session.mu.Unlock()

	<-browserReady

	session.SetState(protocol.StateActive)
	s.sendStatus(conn, protocol.StateActive, "Sender connected, receiving files...")

	for msg := range dataChan {
		if err := conn.WriteMessage(msg.Type, msg.Data); err != nil {
			log.Printf("CLI receiver write error: %v", err)
			break
		}
	}

	close(doneChan)
	session.SetState(protocol.StateComplete)
	log.Printf("CLI receiver completed for session %s", session.Token)
}

// handleBrowserSender handles browser connection for upload
func (s *Server) handleBrowserSender(conn *websocket.Conn, session *Session) {
	defer closeConnGracefully(conn)

	session.SetSenderConn(conn)
	defer session.SetSenderConn(nil)

	log.Printf("Browser sender connected to session %s", session.Token)

	session.mu.Lock()
	dataChan := session.dataChan
	doneChan := session.doneChan
	browserReady := session.browserReady
	session.mu.Unlock()

	if browserReady != nil {
		close(browserReady)
	}

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Browser sender read error: %v", err)
			break
		}

		if messageType == websocket.TextMessage {
			var msg protocol.Message
			if json.Unmarshal(data, &msg) == nil && msg.Type == protocol.MessageTypeComplete {
				log.Printf("Browser sender completed for session %s", session.Token)
				break
			}
		}

		if dataChan != nil {
			select {
			case dataChan <- wsMessage{Type: messageType, Data: data}:
			case <-doneChan:
				return
			}
		}
	}

	if dataChan != nil {
		close(dataChan)
	}

	select {
	case <-doneChan:
	case <-time.After(10 * time.Second):
		log.Printf("Timeout waiting for CLI to drain data for session %s", session.Token)
	}

	log.Printf("Browser sender disconnected from session %s", session.Token)
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
