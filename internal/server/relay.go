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

func enqueueControl(dataChan chan wsMessage, doneChan chan struct{}, msg protocol.Message) bool {
	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}

	select {
	case dataChan <- wsMessage{Type: websocket.TextMessage, Data: data}:
		return true
	case <-doneChan:
		return false
	}
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
	session.fileAckChan = nil
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

		select {
		case dataChan <- wsMessage{Type: messageType, Data: data}:
		case <-doneChan:
			return
		}

		if messageType == websocket.TextMessage {
			var msg protocol.Message
			if json.Unmarshal(data, &msg) == nil && msg.Type == protocol.MessageTypeComplete {
				log.Printf("CLI sender completed for session %s", session.Token)
				break
			}
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
	fileAckChan := make(chan int, 32)

	session.mu.Lock()
	session.dataChan = dataChan
	session.doneChan = doneChan
	session.browserReady = browserReady
	session.fileAckChan = fileAckChan
	session.mu.Unlock()

	<-browserReady

	session.SetState(protocol.StateActive)
	s.sendStatus(conn, protocol.StateActive, "Sender connected, receiving files...")

	go func() {
		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if messageType != websocket.TextMessage {
				continue
			}

			var msg protocol.Message
			if json.Unmarshal(data, &msg) != nil {
				continue
			}
			if msg.Type != protocol.MessageTypeFileReceivedAck {
				continue
			}

			payloadBytes, _ := json.Marshal(msg.Payload)
			var ack protocol.FileReceivedAckPayload
			if json.Unmarshal(payloadBytes, &ack) != nil {
				continue
			}

			select {
			case fileAckChan <- ack.FileID:
			default:
			}
		}
	}()

	var forwardedBytes int64
	var forwardedMessages int64

	for msg := range dataChan {
		if err := conn.WriteMessage(msg.Type, msg.Data); err != nil {
			log.Printf("CLI receiver write error: %v (session=%s forwarded_messages=%d forwarded_bytes=%d)", err, session.Token, forwardedMessages, forwardedBytes)
			break
		}
		forwardedMessages++
		forwardedBytes += int64(len(msg.Data))
	}

	close(doneChan)
	session.SetState(protocol.StateComplete)
	log.Printf("CLI receiver completed for session %s (forwarded_messages=%d forwarded_bytes=%d)", session.Token, forwardedMessages, forwardedBytes)
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
	fileAckChan := session.fileAckChan
	session.mu.Unlock()

	if browserReady != nil {
		close(browserReady)
	}

	var relayedBytes int64
	var relayedMessages int64
	currentFileID := -1
	currentExpectedChunks := 0
	currentExpectedRelayBytes := int64(0)
	currentRelayedChunks := 0
	currentRelayedBytes := int64(0)

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Browser sender read error: %v (session=%s relayed_messages=%d relayed_bytes=%d)", err, session.Token, relayedMessages, relayedBytes)
			break
		}

		if messageType == websocket.BinaryMessage {
			if dataChan != nil {
				select {
				case dataChan <- wsMessage{Type: messageType, Data: data}:
					relayedMessages++
					relayedBytes += int64(len(data))
					currentRelayedChunks++
					currentRelayedBytes += int64(len(data))
				case <-doneChan:
					return
				}
			}
			continue
		}

		if messageType != websocket.TextMessage {
			continue
		}

		var msg protocol.Message
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		switch msg.Type {
		case protocol.MessageTypeMetadata:
			if dataChan != nil {
				select {
				case dataChan <- wsMessage{Type: messageType, Data: data}:
					relayedMessages++
					relayedBytes += int64(len(data))
				case <-doneChan:
					return
				}
			}

			payloadBytes, _ := json.Marshal(msg.Payload)
			var meta struct {
				FileID             int   `json:"file_id"`
				ExpectedChunks     int   `json:"expected_chunks"`
				ExpectedRelayBytes int64 `json:"expected_relay_bytes"`
			}
			if json.Unmarshal(payloadBytes, &meta) == nil {
				currentFileID = meta.FileID
				currentExpectedChunks = meta.ExpectedChunks
				currentExpectedRelayBytes = meta.ExpectedRelayBytes
				currentRelayedChunks = 0
				currentRelayedBytes = 0
			}

		case protocol.MessageTypeFileEnd:
			payloadBytes, _ := json.Marshal(msg.Payload)
			var end protocol.FileEndPayload
			if json.Unmarshal(payloadBytes, &end) != nil {
				log.Printf("Browser sender invalid file_end payload for session %s", session.Token)
				return
			}

			if end.FileID != currentFileID {
				log.Printf("Browser sender file_id mismatch for session %s: end=%d current=%d", session.Token, end.FileID, currentFileID)
				return
			}

			if currentExpectedChunks != 0 && end.ExpectedChunks != currentExpectedChunks {
				log.Printf("Browser sender expected chunk mismatch for session %s file=%d: meta=%d end=%d", session.Token, end.FileID, currentExpectedChunks, end.ExpectedChunks)
				return
			}

			if currentExpectedRelayBytes != 0 && end.ExpectedRelayBytes != currentExpectedRelayBytes {
				log.Printf("Browser sender expected byte mismatch for session %s file=%d: meta=%d end=%d", session.Token, end.FileID, currentExpectedRelayBytes, end.ExpectedRelayBytes)
				return
			}

			if end.ExpectedChunks != currentRelayedChunks || end.ExpectedRelayBytes != currentRelayedBytes {
				log.Printf("Browser sender incomplete stream for session %s file=%d: relayed_chunks=%d expected_chunks=%d relayed_bytes=%d expected_bytes=%d", session.Token, end.FileID, currentRelayedChunks, end.ExpectedChunks, currentRelayedBytes, end.ExpectedRelayBytes)
				return
			}

			committed := protocol.Message{
				Type: protocol.MessageTypeFileCommitted,
				Payload: protocol.FileCommittedPayload{
					FileID:        end.FileID,
					RelayedChunks: currentRelayedChunks,
					RelayedBytes:  currentRelayedBytes,
				},
			}

			if !enqueueControl(dataChan, doneChan, committed) {
				return
			}

			ackTimeout := time.NewTimer(45 * time.Second)
			acked := false
			for !acked {
				select {
				case ackID := <-fileAckChan:
					if ackID == end.FileID {
						acked = true
					}
				case <-ackTimeout.C:
					log.Printf("Timeout waiting for file ack for session %s file=%d", session.Token, end.FileID)
					return
				case <-doneChan:
					ackTimeout.Stop()
					return
				}
			}
			ackTimeout.Stop()

			ackMsg := protocol.Message{
				Type: protocol.MessageTypeFileAcknowledged,
				Payload: protocol.FileReceivedAckPayload{
					FileID: end.FileID,
				},
			}
			ackData, _ := json.Marshal(ackMsg)
			if err := conn.WriteMessage(websocket.TextMessage, ackData); err != nil {
				log.Printf("Failed sending file_acknowledged for session %s file=%d: %v", session.Token, end.FileID, err)
				return
			}

		case protocol.MessageTypeComplete:
			if !enqueueControl(dataChan, doneChan, msg) {
				return
			}
			log.Printf("Browser sender completed for session %s (relayed_messages=%d relayed_bytes=%d)", session.Token, relayedMessages, relayedBytes)
			break
		default:
			if dataChan != nil {
				select {
				case dataChan <- wsMessage{Type: messageType, Data: data}:
					relayedMessages++
					relayedBytes += int64(len(data))
				case <-doneChan:
					return
				}
			}
		}

		if msg.Type == protocol.MessageTypeComplete {
			break
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
