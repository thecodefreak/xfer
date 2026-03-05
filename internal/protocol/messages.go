package protocol

import "time"

// MessageType represents the type of WebSocket message
type MessageType string

const (
	MessageTypeMetadata         MessageType = "metadata"
	MessageTypeChunk            MessageType = "chunk"
	MessageTypeFileEnd          MessageType = "file_end"
	MessageTypeFileCommitted    MessageType = "file_committed"
	MessageTypeFileReceivedAck  MessageType = "file_received_ack"
	MessageTypeFileAcknowledged MessageType = "file_acknowledged"
	MessageTypeComplete         MessageType = "complete"
	MessageTypeStatus           MessageType = "status"
	MessageTypeError            MessageType = "error"
)

// Message is the top-level WebSocket message structure
type Message struct {
	Type    MessageType `json:"type"`
	Payload interface{} `json:"payload"`
}

// SessionRequest is sent by CLI to create a new session
type SessionRequest struct {
	Type      string `json:"type"`      // "send" | "receive"
	Encrypted bool   `json:"encrypted"` // Always true in Xfer
	Password  bool   `json:"password"`  // True if password-protected
}

// SessionResponse is sent by server after creating a session
type SessionResponse struct {
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// FileMetadata contains encrypted file information
type FileMetadata struct {
	EncryptedMeta      []byte `json:"encrypted_metadata"` // Encrypted filename, size, checksum
	PasswordProtected  bool   `json:"password_protected"`
	EncryptedMasterKey []byte `json:"encrypted_master_key,omitempty"` // Only if password-protected
	Salt               []byte `json:"salt,omitempty"`                 // Argon2 salt for password
}

// FileChunk represents a chunk of encrypted file data
type FileChunk struct {
	Data        []byte `json:"data"`
	ChunkID     int    `json:"chunk_id"`
	TotalChunks int    `json:"total_chunks"`
	IsLast      bool   `json:"is_last"`
}

// TransferComplete signals successful transfer completion
type TransferComplete struct {
	Checksum string `json:"checksum"` // SHA-256 hash
}

// TransferStatus represents the current state of a transfer
type TransferStatus struct {
	State     SessionState `json:"state"`
	Message   string       `json:"message,omitempty"`
	BytesSent int64        `json:"bytes_sent,omitempty"`
}

// TransferError represents an error during transfer
type TransferError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// FileEndPayload marks sender-side file stream completion.
type FileEndPayload struct {
	FileID             int   `json:"file_id"`
	ExpectedChunks     int   `json:"expected_chunks"`
	ExpectedRelayBytes int64 `json:"expected_relay_bytes"`
}

// FileCommittedPayload signals the relay accepted all frames for a file.
type FileCommittedPayload struct {
	FileID        int   `json:"file_id"`
	RelayedChunks int   `json:"relayed_chunks"`
	RelayedBytes  int64 `json:"relayed_bytes"`
}

// FileReceivedAckPayload signals receiver finalized and validated a file.
type FileReceivedAckPayload struct {
	FileID int `json:"file_id"`
}

// Error codes
const (
	ErrCodeChecksumMismatch  = "CHECKSUM_MISMATCH"
	ErrCodeTimeout           = "TIMEOUT"
	ErrCodeSessionExpired    = "SESSION_EXPIRED"
	ErrCodeSessionNotFound   = "SESSION_NOT_FOUND"
	ErrCodeInvalidToken      = "INVALID_TOKEN"
	ErrCodeFileTooLarge      = "FILE_TOO_LARGE"
	ErrCodeDecryptionFailed  = "DECRYPTION_FAILED"
	ErrCodeInvalidPassword   = "INVALID_PASSWORD"
	ErrCodeRateLimitExceeded = "RATE_LIMIT_EXCEEDED"
)
