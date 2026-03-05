package server

import (
	"sync"
	"time"

	"xfer/internal/protocol"

	"github.com/gorilla/websocket"
)

// Session represents an active file transfer session
type Session struct {
	Token      string
	Type       protocol.SessionType
	State      protocol.SessionState
	Encrypted  bool // Always true in Xfer
	Password   bool
	CreatedAt  time.Time
	ExpiresAt  time.Time
	SenderConn *websocket.Conn
	RecvConn   *websocket.Conn
	Metadata   *protocol.FileMetadata

	// Data relay channels
	dataChan     chan wsMessage
	doneChan     chan struct{}
	browserReady chan struct{}

	mu sync.Mutex // Protects connection writes
}

// wsMessage holds a WebSocket message with its type
type wsMessage struct {
	Type int
	Data []byte
}

// SessionStore manages all active sessions
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewSessionStore creates a new session store
func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
}

// Create creates a new session
func (s *SessionStore) Create(token string, sessionType protocol.SessionType, password bool) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	session := &Session{
		Token:     token,
		Type:      sessionType,
		State:     protocol.StatePending,
		Encrypted: true,
		Password:  password,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}

	s.sessions[token] = session
	return session
}

// Get retrieves a session by token
func (s *SessionStore) Get(token string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[token]
	return session, exists
}

// Delete removes a session
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, exists := s.sessions[token]; exists {
		// Close WebSocket connections if they exist
		if session.SenderConn != nil {
			session.SenderConn.Close()
		}
		if session.RecvConn != nil {
			session.RecvConn.Close()
		}
		delete(s.sessions, token)
	}
}

// CleanupExpired removes expired sessions
func (s *SessionStore) CleanupExpired() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	count := 0

	for token, session := range s.sessions {
		if now.After(session.ExpiresAt) || session.State.IsTerminal() {
			// Close connections
			if session.SenderConn != nil {
				session.SenderConn.Close()
			}
			if session.RecvConn != nil {
				session.RecvConn.Close()
			}
			delete(s.sessions, token)
			count++
		}
	}

	return count
}

// Count returns the number of active sessions
func (s *SessionStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// SetSenderConn sets the sender's WebSocket connection
func (sess *Session) SetSenderConn(conn *websocket.Conn) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.SenderConn = conn
}

// SetRecvConn sets the receiver's WebSocket connection
func (sess *Session) SetRecvConn(conn *websocket.Conn) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.RecvConn = conn
}

// SetState updates the session state
func (sess *Session) SetState(state protocol.SessionState) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.State = state
}

// GetState returns the current state
func (sess *Session) GetState() protocol.SessionState {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.State
}

// IsExpired checks if the session has expired
func (sess *Session) IsExpired() bool {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return time.Now().After(sess.ExpiresAt)
}
