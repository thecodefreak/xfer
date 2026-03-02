package protocol

// SessionState represents the current state of a transfer session
type SessionState string

const (
	// StatePending indicates session is created but no connections yet
	StatePending SessionState = "pending"

	// StateActive indicates both parties connected, transfer in progress
	StateActive SessionState = "active"

	// StateComplete indicates transfer finished successfully
	StateComplete SessionState = "complete"

	// StateFailed indicates transfer failed with an error
	StateFailed SessionState = "failed"

	// StateExpired indicates session TTL exceeded
	StateExpired SessionState = "expired"
)

// IsTerminal returns true if the state is a terminal state
func (s SessionState) IsTerminal() bool {
	return s == StateComplete || s == StateFailed || s == StateExpired
}

// SessionType represents the type of session
type SessionType string

const (
	SessionTypeSend    SessionType = "send"
	SessionTypeReceive SessionType = "receive"
)
