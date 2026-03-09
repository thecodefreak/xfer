# Xfer - Security Analysis

## Threat Model

### Trust Assumptions

**Trusted:**
- End-user devices (sender/receiver)
- TLS/HTTPS transport layer
- Operating system crypto libraries

**Untrusted:**
- Relay server operator (honest-but-curious model)
- Network infrastructure (assume monitoring)
- Third-party observers

**User Responsibility:**
- Endpoint security (malware prevention)
- Password strength (if using password protection)
- QR code privacy (prevent shoulder surfing)

---

## Security Architecture

### End-to-End Encryption (Mandatory)

**Algorithm:** AES-256-GCM  
**Key Derivation:** HKDF-SHA256  
**Token Generation:** Cryptographically secure random (256-bit)

**Encryption Flow:**

1. **Key Generation (Client-side)**
   - Master key: 32 random bytes (256-bit)
   - Never transmitted to server
   - Sent in URL fragment only (client-to-client)

2. **Key Derivation (HKDF)**
   ```
   Master Key (32 bytes)
       ├─ HKDF-SHA256 → File Encryption Key (32 bytes)
       ├─ HKDF-SHA256 → Metadata Encryption Key (32 bytes)
       ├─ HKDF-SHA256 → File Nonce Prefix (4 bytes)
       └─ HKDF-SHA256 → Metadata Nonce Prefix (4 bytes)
   ```

3. **Nonce Construction**
   - Nonce = Prefix (4 bytes) + Counter (8 bytes) = 12 bytes
   - Unique per chunk (counter increments)
   - Prevents nonce reuse

4. **Chunk Encryption**
   - File split into 64KB chunks
   - Each encrypted independently with unique nonce
   - GCM provides authentication (AEAD)

5. **Metadata Encryption**
   - Filename, size, checksum encrypted separately
   - Server never sees plaintext metadata

**Security Properties:**
- **Confidentiality:** AES-256 encryption
- **Integrity:** GCM authentication tags
- **Forward Secrecy:** Per-session keys
- **Server Blindness:** Server sees only ciphertext

---

### Optional Password Protection

**Purpose:** Additional security layer for sensitive transfers

**Algorithm:** Argon2id (memory-hard, side-channel resistant)

**Flow:**

1. **Sender Side:**
   - User provides password via `--password` flag
   - Argon2id derives password key (32 bytes)
   - Master key encrypted with password key (AES-256-GCM)
   - Encrypted master key transmitted to receiver
   - Password never transmitted

2. **Receiver Side:**
   - Prompts for password
   - Derives same password key using Argon2id
   - Decrypts master key
   - Proceeds with normal E2E decryption

**Argon2id Parameters:**
- Time cost: 3 iterations
- Memory: 64MB
- Parallelism: 4 threads
- Salt: 16 random bytes (unique per transfer)

**Security Properties:**
- **Brute-force Resistant:** Memory-hard function
- **Side-channel Resistant:** Data-independent execution
- **Zero-knowledge:** Server never sees password

**Threats:**
- **Weak Passwords:** User responsibility
- **Password Reuse:** No enforcement mechanism

---

## Session Security

### Session Tokens

**Strength:** 256-bit cryptographically secure random  
**Encoding:** Base64url (URL-safe)  
**Lifetime:** 5 minutes default (configurable)  
**Usage:** Single-use (invalidated after transfer)

**Generation:**
```go
token := make([]byte, 32)  // 256 bits
rand.Read(token)            // crypto/rand
encoded := base64.URLEncoding.EncodeToString(token)
```

**Properties:**
- **Unpredictable:** Cryptographic RNG
- **High Entropy:** 2^256 possible values
- **Single-use:** Prevents replay attacks
- **Time-limited:** Auto-expiry prevents stale sessions

### Session Lifecycle

1. **Creation:** Client requests session → server generates token
2. **Active:** Both parties connect via WebSocket
3. **Transfer:** Encrypted data streams through server
4. **Completion:** Session destroyed immediately
5. **Expiry:** Automatic cleanup after TTL (5 min default)

**Cleanup:**
- Background task runs every 30 seconds
- Removes expired sessions
- Frees memory
- Prevents session exhaustion attacks

---

## Transport Security

### TLS Requirements

**Minimum:** TLS 1.2  
**Recommended:** TLS 1.3  
**Cipher Suites:** Modern, forward-secret ciphers only

**Production Deployment:**
- Reverse proxy handles TLS termination (nginx/Traefik/Caddy)
- Automatic certificate renewal (Let's Encrypt)
- HSTS headers recommended
- Server behind reverse proxy (HTTP → HTTPS upgrade)

**Development:**
- `--insecure` flag allows HTTP (testing only)
- Warning displayed to user
- Never use in production

---

## Attack Mitigation

### 1. Rate Limiting

**Implementation:** Token bucket algorithm  
**Default:** 10 requests/second per IP, burst 20  
**Scope:** Per-IP enforcement via `X-Forwarded-For` header

**Protected Endpoints:**
- `POST /api/sessions` (session creation)
- `GET /download/:token` (download page)
- `GET /upload/:token` (upload page)

**Prevents:**
- Brute-force token guessing
- Session exhaustion attacks
- Resource exhaustion

### 2. CSRF Protection

**Mechanism:** Origin header validation + CSRF tokens

**Implementation:**
- Server validates `Origin` header on state-changing requests
- CSRF tokens embedded in forms
- Double-submit cookie pattern
- SameSite cookie attribute

**Protected Endpoints:**
- `POST /api/sessions`
- File upload endpoint

### 3. Input Sanitization

**Filename Sanitization:**
```go
// Remove path traversal attempts
filename = filepath.Base(filename)

// Remove null bytes
filename = strings.ReplaceAll(filename, "\x00", "")

// Limit length
if len(filename) > 255 {
    filename = filename[:255]
}

// Block dangerous patterns
if strings.Contains(filename, "..") {
    return error
}
```

**Prevents:**
- Path traversal attacks
- Directory escape
- Null byte injection

### 4. File Size Limits

**Default:** 200MB  
**Server-configurable:** `XFER_MAX_SIZE` environment variable  
**Enforcement:** Both client and server sides

**Prevents:**
- Memory exhaustion
- Disk space attacks
- DoS via large files

### 5. Content Validation

**Checksum Verification:**
- SHA-256 hash calculated client-side
- Transmitted with encrypted metadata
- Automatically verified on receive
- Mismatch triggers error

**Prevents:**
- File corruption
- Tampering (within E2E scope)
- Incomplete transfers

### 6. Per-file Commit Handshake

**Mechanism:** `file_end` -> `file_committed` -> `file_received_ack` -> `file_acknowledged`

**Flow:**
- Uploader sends `file_end` with expected relayed bytes/chunks for that file
- Relay validates observed relayed totals before emitting `file_committed`
- Receiver finalizes checksum and sends `file_received_ack`
- Relay confirms back to uploader with `file_acknowledged`

**Prevents:**
- Random completion races on large transfers
- Premature close signaling before final chunk drain
- False-positive success when receiver has not finalized file integrity

### 7. Upload UI Mutation Lock

**Mechanism:** Browser uploader enters a locked state after transfer start.

**Flow:**
- `Add more files`, `Clear all`, file picker, and drag-drop mutations are blocked while upload is active
- File list can be edited again only if transfer fails and uploader returns to retry state

**Prevents:**
- Mid-transfer client-side file list mutation that can desync user intent from transmitted stream
- Confusing partial-selection state transitions during active encrypted relay

### 8. Release Container CVE Gate

**Mechanism:** Release workflow runs vulnerability scanning on Dockerfile base images.

**Policy:**
- Scanner: Trivy
- Scope: base images used by Dockerfile build/runtime stages
- Threshold: fail release on HIGH or CRITICAL findings

**Prevents:**
- Publishing release images with known severe base-image vulnerabilities
- Regressions where `latest` and version tags carry avoidable CVE exposure

### 9. Browser Download Message Queue

**Mechanism:** Browser download page queues all incoming WebSocket messages and processes them sequentially.

**Problem Solved:**
- WebSocket onmessage handlers in JavaScript are non-blocking for async functions
- Multiple chunks arriving rapidly would each start async decryption in parallel
- The "complete" or "close" event could fire before all decryptions finished
- This caused false "Transfer incomplete" errors on large files (e.g., 69MB of 70MB)

**Flow:**
- All incoming messages (binary chunks, metadata, complete, close) are queued
- A single processor works through the queue sequentially
- File completion only triggers after all queued chunks are processed
- Error/finalize states only set after queue is empty

**Prevents:**
- Race conditions between async chunk decryption and connection close
- False incomplete transfer errors on fast transfers
- Lost data when processing lags behind network receive speed

### 10. Strict Download Completion Handshake

**Mechanism:** Browser sends `download_ack` only after all files are finalized locally; relay waits for it before session close.

**Flow:**
- Relay forwards all encrypted data and `complete` to browser receiver
- Browser decrypts/finalizes and then emits `download_ack`
- Relay waits up to 45 seconds for ack before marking session complete
- Missing/invalid/timeout ack marks session failed (strict mode)

**Prevents:**
- Premature WebSocket close before browser-side finalization under real network latency
- False-positive transfer success when browser has not fully processed payload
- Silent truncation at tail-end of large downloads

### 11. Sender Finalization Gating and Backpressure Hardening

**Mechanism:** Sender success is gated on relay terminal status, and binary frame writes use a longer deadline than control frames.

**Flow:**
- CLI sender streams encrypted chunks and sends `complete`
- CLI sender then waits for relay terminal status (`complete` or `failed`)
- Relay returns `complete` only after browser `download_ack`; otherwise returns `failed` with reason
- Sender binary writes tolerate slow receiver backpressure better via extended binary write deadline

**Prevents:**
- False sender-side success when receiver has not actually finalized transfer
- Spurious sender disconnects on large transfers when downstream drain is temporarily slow
- Ambiguous abnormal-closure outcomes without an explicit terminal state

---

## Vulnerability Analysis

### Addressed Vulnerabilities

| Vulnerability | Mitigation | Status |
|---------------|------------|--------|
| Man-in-the-Middle | TLS + E2E encryption | Protected |
| Server Data Access | Mandatory E2E encryption | Protected |
| Session Hijacking | 256-bit tokens, single-use | Protected |
| Replay Attacks | Nonces, session expiry | Protected |
| Brute Force | Rate limiting, strong tokens | Protected |
| Path Traversal | Filename sanitization | Protected |
| CSRF | Origin validation, tokens | Protected |
| File Tampering | SHA-256 checksums | Protected |
| DoS (File Size) | 200MB limit | Protected |
| DoS (Rate) | Token bucket limiting | Protected |

### Known Limitations

| Threat | Risk Level | Mitigation |
|--------|------------|------------|
| Endpoint Compromise | High | User responsibility, no technical fix |
| Weak Passwords | Medium | User education, no enforcement |
| Shoulder Surfing | Medium | QR hide/reveal feature |
| Server DoS | Medium | Reverse proxy rate limiting |
| Browser password support | Low | CLI-only password protection until browser KDF added |
| Quantum Attacks | Low | AES-256 is quantum-resistant (Grover's) |

### Out of Scope

- **User Authentication** - By design, sessionless
- **Long-term Storage** - Files never stored, temporary relay only
- **Transfer History Sync** - Local only, privacy-first
- **Virus Scanning** - Optional hook available, not built-in

---

## Privacy Considerations

### Data Minimization

**Server Never Sees:**
- File contents (encrypted)
- Filenames (encrypted metadata)
- Crypto keys (URL fragment, client-only)
- Passwords (used locally only)

**Server Sees:**
- Session tokens (necessary for routing)
- File sizes (encrypted payload size)
- IP addresses (necessary for networking)
- Timestamps (session management)

### Audit Logging (Server-side, Opt-in)

**If Enabled:**
- Session creation/completion events
- Security events (rate limit hits, invalid tokens)
- No file contents or metadata
- Privacy-preserving (no PII beyond IP)

**Use Case:** Compliance, abuse detection

---

## Secure Development Practices

### Code Security

- **No Hardcoded Secrets** - All config via env/file
- **Secure Defaults** - Encryption mandatory, secure timeouts
- **Input Validation** - All user input sanitized
- **Error Handling** - No sensitive data in errors
- **Dependency Management** - Minimal deps, reviewed

### Cryptography

- **Standard Libraries** - Go crypto/* packages
- **No Custom Crypto** - Proven algorithms only
- **Secure RNG** - crypto/rand, never math/rand
- **Key Management** - Ephemeral keys, proper derivation

### Testing

- **Unit Tests** - Core crypto and protocol
- **Integration Tests** - Full transfer flows
- **Security Tests** - CSRF, rate limiting, path traversal
- **Fuzzing** - Input validation (planned)

---

## Incident Response

### Security Event Handling

**Rate Limit Exceeded:**
- Log event
- Return HTTP 429
- Temporary IP block (if repeated)

**Invalid Token:**
- Log event
- Return HTTP 404 (prevent enumeration)
- No sensitive information leaked

**Session Expired:**
- Clean up resources
- Return clear error to client
- User retries with new session

### Vulnerability Disclosure

**Process:**
1. Report to security@thecodefreak.in
2. Acknowledge within 24 hours
3. Fix critical issues within 7 days
4. Coordinated disclosure after fix

**Scope:**
- Server vulnerabilities
- Protocol weaknesses
- Crypto implementation flaws
- DoS vectors

---

## Compliance Considerations

### GDPR Compliance

- **Data Minimization** - Only necessary data processed
- **Purpose Limitation** - Data used only for transfer
- **Storage Limitation** - No permanent storage
- **User Control** - Opt-out of history
- **Right to Deletion** - `xfer history clear`

**Note:** Self-hosted deployment gives full data control

### Security Best Practices

- **OWASP Top 10** - Addressed common vulnerabilities
- **CWE/SANS Top 25** - Mitigated critical weaknesses
- **NIST Guidelines** - Follows crypto recommendations

---

## Operational Security

### Server Hardening

**Recommended:**
- Run as non-root user
- Minimal container image (distroless)
- Read-only filesystem where possible
- No shell in production container
- Resource limits (memory, CPU)
- Network segmentation

**Monitoring:**
- Health check endpoint
- Metrics export (Prometheus compatible)
- Log aggregation (JSON structured logs)
- Alert on unusual patterns

### Backup & Recovery

**No Data to Backup:**
- Stateless design
- No persistent storage
- Sessions in-memory only
- Configuration via environment

**Disaster Recovery:**
- Redeploy server
- No data loss (nothing stored)
- Users retry failed transfers

---

## Security Roadmap

### Implemented

- Mandatory E2E encryption
- Optional password protection
- 256-bit session tokens
- Rate limiting
- CSRF protection
- Checksum verification
- Input sanitization

### Planned (Future)

- Security audit (third-party)
- Penetration testing
- Bug bounty program
- Security.txt file
- CVE monitoring automation

### Under Consideration

- Multi-factor authentication (for future account features)
- Hardware security key support
- Transfer expiry (auto-delete after download)
- IP allowlist/blocklist

---

## Conclusion

Xfer prioritizes security through:
1. **Mandatory encryption** - No plaintext option
2. **Zero-knowledge server** - Cannot access file contents
3. **Defense in depth** - Multiple layers of protection
4. **Privacy by design** - Minimal data collection
5. **Secure defaults** - Safe out-of-the-box configuration

**Trust Model:** "Don't trust the server, don't trust the network, trust the crypto."
