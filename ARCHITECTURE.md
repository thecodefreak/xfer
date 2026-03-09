# Xfer - Technical Architecture

## System Overview

Xfer is a file transfer system built on three core principles:
1. **End-to-end encryption** - Server never sees plaintext
2. **Ephemeral sessions** - No persistent storage
3. **NAT-friendly** - Works behind firewalls

---

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Xfer Ecosystem                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌──────────────┐         ┌──────────────┐         ┌─────────┐ │
│  │              │         │              │         │         │ │
│  │   Browser    │◄───────►│    Relay     │◄───────►│   CLI   │ │
│  │   (Mobile)   │  HTTPS  │   Server     │   WSS   │ (Desktop│ │
│  │              │  /WSS   │              │         │         │ │
│  └──────────────┘         └──────────────┘         └─────────┘ │
│        │                         │                       │      │
│        │                         │                       │      │
│   Scans QR              Relays encrypted           Generates    │
│   Decrypts              chunks only                Encrypts     │
│   locally                                          locally      │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘

Key: All file data is encrypted before leaving sender
```

---

## Component Architecture

### 1. CLI Client

**Language:** Go  
**Framework:** Cobra (CLI), gorilla/websocket  

**Directory Structure:**
```
cmd/xfer/
├── main.go                    # Entry point
└── commands/
    ├── root.go                # Root command + global flags
    ├── send.go                # Send command
    ├── receive.go             # Receive command
    ├── config.go              # Config management
    └── version.go             # Version info
```

**Core Modules:**

```
internal/
├── client/
│   ├── client.go              # HTTP/WebSocket client
│   ├── send.go                # Send logic
│   ├── receive.go             # Receive logic
│   └── progress.go            # Progress tracking
├── crypto/
│   ├── encrypt.go             # AES-256-GCM encryption
│   ├── password.go            # Password-based encryption (Argon2id)
│   ├── checksum.go            # SHA-256 checksums
│   └── token.go               # Token generation
├── config/
│   └── config.go              # Config file management
└── protocol/
    ├── messages.go            # Message types
    └── states.go              # Session states
```

**Key Dependencies:**
```go
github.com/spf13/cobra         // CLI framework
github.com/gorilla/websocket   // WebSocket client
github.com/skip2/go-qrcode     // QR code generation
golang.org/x/crypto/argon2     // Password KDF
golang.org/x/crypto/hkdf       // Key derivation
gopkg.in/yaml.v3               // Config parsing
```

**Data Flow (Send):**
```
1. User runs: xfer send file.txt --password
2. CLI prompts for password (if --password)
3. Generate master key (32 random bytes)
4. Derive encryption keys (HKDF)
5. If password: encrypt master key with password-derived key
6. Calculate SHA-256 checksum
7. Encrypt metadata (filename, size, checksum)
8. Request session from server
9. Server responds with token + URL
10. Generate QR code with URL + key fragment
11. Display QR in terminal
12. Open WebSocket to server
13. Wait for receiver connection
14. Stream file in encrypted 64KB chunks
15. Send `complete` message
16. Wait for relay terminal status (`complete`/`failed`)
17. Close connection
```

**Data Flow (Receive):**
```
1. User runs: xfer receive
2. Request receive session from server
3. Server responds with token + URL
4. Generate QR code
5. Display in terminal
6. Open WebSocket and wait
7. Browser uploads file (encrypted)
8. Receive chunks via WebSocket
9. Decrypt chunks in real-time
10. Verify checksum
11. Write to disk
12. Close connection
```

---

### 2. Relay Server

**Language:** Go  
**HTTP Server:** net/http (standard library)  
**WebSocket:** gorilla/websocket  

**Directory Structure:**
```
cmd/xfer-server/
└── main.go                    # Server entry point

internal/server/
├── server.go                  # HTTP server setup
├── session.go                 # Session store (in-memory)
├── handlers.go                # HTTP/WebSocket handlers
├── middleware.go              # Rate limiting, CSRF, logging
├── relay.go                   # WebSocket message relay
├── ratelimit.go               # Rate limiting implementation
├── csrf.go                    # CSRF protection
└── templates.go               # HTML templates
```

**Session Store (In-Memory):**
```go
type SessionStore struct {
    mu       sync.RWMutex
    sessions map[string]*Session
}

type Session struct {
    Token      string
    Type       string              // "send" | "receive"
    State      string              // "pending" | "active" | "complete"
    Encrypted  bool                // Always true
    Password   bool                // True if password-protected
    CreatedAt  time.Time
    ExpiresAt  time.Time
    SenderConn *websocket.Conn    // WebSocket from CLI sender
    RecvConn   *websocket.Conn    // WebSocket from browser/CLI receiver
    Metadata   *FileMetadata      // Encrypted metadata
}
```

**Endpoints:**

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/sessions` | Create new session |
| GET | `/api/sessions/:token/ws` | WebSocket upgrade |
| GET | `/download/:token` | Serve download page |
| GET | `/upload/:token` | Serve upload page |
| GET | `/health` | Health check |

**Request Flow (Session Creation):**
```
Client → POST /api/sessions
         {
           "type": "send",
           "encrypted": true,
           "password": true
         }

Server → Generate 256-bit token
      → Create Session object
      → Store in SessionStore
      → Start cleanup timer
      → Return response:
         {
           "token": "abc123...",
           "url": "https://xfer.example.com/download/abc123...",
           "expires_at": "2026-03-02T12:35:00Z"
         }
```

**WebSocket Relay:**
```
┌─────────┐                  ┌────────┐                  ┌─────────┐
│ Sender  │                  │ Server │                  │Receiver │
│   CLI   │                  │        │                  │ Browser │
└────┬────┘                  └───┬────┘                  └────┬────┘
     │                           │                            │
     │ WebSocket CONNECT         │                            │
     ├──────────────────────────►│                            │
     │                           │                            │
     │                           │     WebSocket CONNECT      │
     │                           │◄───────────────────────────┤
     │                           │                            │
     │                           │ (Both connected = "active") │
     │                           │                            │
     │ FileMetadata (encrypted)  │                            │
     ├──────────────────────────►├───────────────────────────►│
     │                           │                            │
     │ FileChunk (encrypted)     │                            │
     ├──────────────────────────►├───────────────────────────►│
     │                           │                            │
     │ FileChunk (encrypted)     │                            │
     ├──────────────────────────►├───────────────────────────►│
     │                           │                            │
     │ Complete message          │                            │
     ├──────────────────────────►├───────────────────────────►│
     │                           │                            │
     │         CLOSE             │           CLOSE            │
     ├──────────────────────────►├───────────────────────────►│
     │                           │                            │
     │                      (Session destroyed)               │
```

**Session Cleanup:**
- Background goroutine runs every 30 seconds
- Checks `ExpiresAt` for each session
- Removes expired sessions
- Closes WebSocket connections
- Frees memory

**Security Middleware:**

1. **Rate Limiting:**
   ```go
   type RateLimiter struct {
       limiters sync.Map  // map[string]*rate.Limiter
       rate     float64   // 10 requests/sec
       burst    int       // 20 requests burst
   }
   ```

2. **CSRF Protection:**
   - Validate `Origin` header matches server URL
   - Check CSRF token on state-changing requests

3. **Request Logging:**
   - Structured JSON logs
   - No sensitive data logged

**Concurrency Model:**
- One goroutine per WebSocket connection
- Session store protected by RWMutex
- Write mutex per WebSocket connection (prevent concurrent writes)

---

### 3. Web UI (Browser)

**Technology:** Vanilla JavaScript + Go templates  
**Crypto:** WebCrypto API  

**Download Page Flow:**
```
1. User scans QR → Opens URL: /download/:token#key=<master_key>
2. Browser loads page template
3. JavaScript extracts key from URL fragment (never sent to server)
4. If password-protected:
   a. Show password input
   b. User enters password
   c. Derive password key (Argon2id in WASM/JS)
   d. Decrypt master key
5. Open WebSocket to server
6. Receive encrypted metadata
7. Decrypt metadata locally (filename, size, checksum)
8. Display file info to user
9. User clicks "Download"
10. Receive encrypted chunks via WebSocket
11. Decrypt each chunk locally (AES-256-GCM)
12. Build file in memory
13. Verify SHA-256 checksum
14. Trigger browser download (Blob API)
15. Send `download_ack` after local finalize
16. Close connection
```

**Upload Page Flow:**
```
1. User scans QR → Opens URL: /upload/:token#key=<master_key>
2. Extract key from fragment
3. User selects file(s)
4. Uploader enters locked transfer state (file list mutation actions hidden/blocked)
5. For each file (sequential):
   a. Calculate SHA-256 checksum
   b. Encrypt metadata (filename, size, checksum)
   c. Send encrypted metadata (+ file_id)
   d. Stream file in 64KB chunks
   e. Encrypt each chunk (AES-256-GCM)
   f. Send encrypted chunks
   g. Send `file_end` with expected relayed byte/chunk totals
   h. Wait for relay/receiver confirmation (`file_acknowledged`)
   i. Show progress/finalizing state
  6. Send session `complete` after all files are acknowledged
  7. Close connection
```

**JavaScript Modules:**
```
/static/
├── crypto.js                  # WebCrypto API wrappers
│   ├── deriveKeys()          # HKDF key derivation
│   ├── encrypt()             # AES-256-GCM encryption
│   ├── decrypt()             # AES-256-GCM decryption
│   ├── checksum()            # SHA-256 hashing
│   └── passwordDecrypt()     # Argon2id + AES-256-GCM
├── download.js                # Download page logic
├── upload.js                  # Upload page logic
└── ui.js                      # UI helpers (progress, errors)
```

**WebCrypto Implementation:**
```javascript
// Key derivation (HKDF)
async function deriveKeys(masterKey) {
  const keyMaterial = await crypto.subtle.importKey(
    'raw', masterKey, 'HKDF', false, ['deriveKey', 'deriveBits']
  );

  const fileKey = await crypto.subtle.deriveKey(
    { name: 'HKDF', hash: 'SHA-256', salt: new Uint8Array(), info: utf8('file') },
    keyMaterial,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt']
  );

  const metaKey = await crypto.subtle.deriveKey(
    { name: 'HKDF', hash: 'SHA-256', salt: new Uint8Array(), info: utf8('metadata') },
    keyMaterial,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt']
  );

  return { fileKey, metaKey };
}

// Encryption
async function encrypt(data, key, nonce) {
  return await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: nonce, tagLength: 128 },
    key,
    data
  );
}
```

---

## Protocol Specification

### WebSocket Message Format

**All messages are JSON:**

```json
{
  "type": "metadata | file_end | file_committed | file_received_ack | file_acknowledged | download_ack | complete | error | status",
  "payload": { ... }
}
```

**Message Types:**

1. **Metadata** (Sender → Receiver)
```json
{
  "type": "metadata",
  "payload": {
    "encrypted_metadata": "base64-encoded-ciphertext",
    "password_protected": true,
    "encrypted_master_key": "base64..."  // Only if password-protected
  }
}
```

2. **Chunk** (Sender → Receiver)
```json
{
  "type": "chunk",
  "payload": {
    "data": "base64-encoded-encrypted-chunk",
    "chunk_id": 42,
    "total_chunks": 100,
    "is_last": false
  }
}
```

3. **Complete** (Sender → Receiver)
```json
{
  "type": "complete",
  "payload": {
    "checksum": "sha256-hash"
  }
}
```

4. **Status** (Server ↔ Clients)
```json
{
  "type": "status",
  "payload": {
    "state": "pending | active | complete | failed",
    "message": "Waiting for receiver..."
  }
}
```

5. **Error** (Any → Any)
```json
{
  "type": "error",
  "payload": {
    "code": "CHECKSUM_MISMATCH | TIMEOUT | ...",
    "message": "Checksum verification failed"
  }
}
```

---

## Data Flow Diagrams

### Send Flow (Complete)

```
┌─────────┐                    ┌────────┐                    ┌─────────┐
│  CLI    │                    │ Server │                    │ Browser │
│ (Sender)│                    │        │                    │(Receiver)│
└────┬────┘                    └───┬────┘                    └────┬────┘
     │                             │                              │
     │ 1. POST /api/sessions       │                              │
     │    {type: "send"}            │                              │
     ├────────────────────────────►│                              │
     │                             │                              │
     │ 2. Session created          │                              │
     │    {token, url}             │                              │
     │◄────────────────────────────┤                              │
     │                             │                              │
     │ 3. Generate QR code         │                              │
     │    (URL + key fragment)     │                              │
     │                             │                              │
     │ 4. Display QR               │                              │
     │    ┌─────────────┐          │                              │
     │    │ ▄▄▄ ▄ ▄ ▄▄▄ │          │                              │
     │    │ ███ ▄ █ ███ │          │                              │
     │    │ ▄▄▄ ▄ █ ▄▄▄ │          │                              │
     │    └─────────────┘          │                              │
     │                             │                              │
     │ 5. WS CONNECT               │                              │
     ├────────────────────────────►│                              │
     │                             │                              │
     │                             │          6. User scans QR    │
     │                             │             GET /download/:token#key=...
     │                             │◄─────────────────────────────┤
     │                             │                              │
     │                             │          7. HTML page        │
     │                             ├─────────────────────────────►│
     │                             │                              │
     │                             │          8. WS CONNECT       │
     │                             │◄─────────────────────────────┤
     │                             │                              │
     │ 9. Status: "active"         │         Status: "active"     │
     │◄────────────────────────────┼─────────────────────────────►│
     │                             │                              │
     │ 10. Metadata (encrypted)    │                              │
     ├────────────────────────────►├─────────────────────────────►│
     │                             │          11. Decrypt locally │
     │                             │              Show file info  │
     │                             │                              │
     │                             │          12. User clicks DL  │
     │                             │                              │
     │ 13. Chunk 1 (encrypted)     │                              │
     ├────────────────────────────►├─────────────────────────────►│
     │                             │          14. Decrypt chunk   │
     │                             │              Update progress │
     │                             │                              │
     │ 15. Chunk 2...N             │                              │
     ├────────────────────────────►├─────────────────────────────►│
     │                             │                              │
     │ 16. Complete {checksum}     │                              │
     ├────────────────────────────►├─────────────────────────────►│
     │                             │          17. Verify checksum │
     │                             │              Trigger download│
     │                             │                              │
      │                             │          18. download_ack   │
      │                             │◄─────────────────────────────┤
      │ 19. CLOSE                   │              CLOSE           │
      ├────────────────────────────►├─────────────────────────────►│
      │                             │                              │
      │                        (Session destroyed)                 │
```

---

## Deployment Architecture

### Docker Container

**Dockerfile (Multi-stage):**
```dockerfile
# Stage 1: Build
FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/xfer-server ./cmd/xfer-server

# Stage 2: Runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/xfer-server /usr/local/bin/
EXPOSE 8080
ENTRYPOINT ["xfer-server"]
```

**Production Stack:**
```
                    ┌─────────────┐
                    │   Internet  │
                    └──────┬──────┘
                           │ :443 (HTTPS)
                           ▼
                    ┌─────────────┐
                    │   Traefik   │  (TLS termination, routing)
                    │   (Reverse  │
                    │    Proxy)   │
                    └──────┬──────┘
                           │ :8080 (HTTP)
                           ▼
                    ┌─────────────┐
                    │ Xfer Server │  (Docker container)
                    │   - Port:   │
                    │     8080    │
                    └─────────────┘
```

**Docker Compose:**
```yaml
services:
  xfer:
    image: ghcr.io/thecodefreak/xfer:latest
    container_name: xfer
    environment:
      XFER_BASE_URL: https://xfer.example.com
      XFER_PORT: 8080
      XFER_SESSION_TTL: 5m
      XFER_MAX_SIZE: 209715200  # 200MB
    ports:
      - "127.0.0.1:8080:8080"
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "wget", "-q", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3
```

---

### Release Pipeline

- Triggered by tags matching `release-v*`
- Derives release version from tag and publishes artifacts as `vX.Y.Z`
- Builds Linux, Windows, and macOS binaries for `xfer` and `xfer-server`
- Publishes multi-arch Docker image (`linux/amd64`, `linux/arm64`) to `ghcr.io/thecodefreak/xfer`
- Runs CVE gate on Dockerfile base images and fails release on HIGH/CRITICAL findings

---

## Performance Characteristics

### Memory Usage

**Server:**
- Base: ~10MB (Go runtime)
- Per session: ~1MB (WebSocket buffers)
- Peak: Base + (Concurrent sessions × 1MB)

**File Size Impact:**
- Files streamed in 64KB chunks
- Not loaded entirely into memory
- Memory usage independent of file size

### Throughput

**Bottlenecks:**
1. Network bandwidth (primary)
2. Encryption/decryption (CPU-bound)
3. WebSocket message overhead (minimal)

**Expected Performance:**
- Local network: ~100 MB/s (limited by encryption)
- Internet: Bandwidth-limited (typically 10-50 MB/s)

### Scalability

**Horizontal Scaling:**
- Stateless server design (session store in-memory)
- Sticky sessions required (WebSocket affinity)
- Or: Use Redis for distributed session store (future enhancement)

**Vertical Scaling:**
- CPU: More concurrent encryptions
- Memory: More concurrent sessions
- Network: Higher aggregate throughput

---

## Technology Choices

### Why Go?

- **Native concurrency** - Goroutines for WebSocket handling
- **Strong crypto library** - Trusted crypto/* packages
- **Single binary** - Easy deployment
- **Good performance** - Fast encryption, low overhead
- **Type safety** - Prevents many bugs

### Why WebSockets?

- **Bidirectional** - Real-time communication
- **Low latency** - No HTTP overhead per chunk
- **NAT-friendly** - Client initiates connection
- **Browser support** - Native WebSocket API

### Why Vanilla JS?

- **Lightweight** - No framework bloat
- **Auditable** - Simple, easy to review
- **Fast** - No build step, direct execution
- **Compatible** - Works in all modern browsers

---

## Future Architecture Considerations

### Potential Enhancements

**Distributed Session Store:**
- Redis for multi-server deployments
- Session replication
- Horizontal scaling

**P2P Mode:**
- Direct connections (WebRTC)
- Server only for signaling
- Lower latency, higher throughput

**Mobile Apps:**
- Native iOS/Android apps
- Better UX than web
- Background transfers

**Transfer Resume:**
- Chunk-level tracking
- Resume from interruption
- More reliable for large files

---

## Conclusion

Xfer's architecture prioritizes:
1. **Security** - Mandatory E2E encryption, zero-knowledge server
2. **Simplicity** - Clean code, minimal dependencies
3. **Reliability** - Proper error handling, graceful degradation
4. **Privacy** - No data retention
5. **Usability** - QR codes, progress tracking, friendly UI

The design is **production-ready** while remaining **maintainable** and **extensible** for future enhancements.
