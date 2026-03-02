# Xfer

**Secure, encrypted file transfer via QR codes**

Xfer enables seamless file sharing between devices using QR codes and end-to-end encryption. Send files from your desktop to your phone, or vice versa, with complete privacy.

## Features

- 🔒 **Mandatory end-to-end encryption** (AES-256-GCM)
- 🔑 **Optional password protection** for sensitive files
- 📱 **QR code transfers** - scan and go
- 🚀 **Works across networks** - NAT and firewall friendly
- 🔐 **Zero-knowledge server** - relay never sees plaintext
- ✅ **Automatic checksum verification**
- 📊 **Real-time progress tracking**
- 🗂️ **Transfer history** with privacy controls
- 🎨 **Modern, friendly UI**

## Quick Start

### Installation

```bash
# From source
go install xfer/cmd/xfer@latest

# Or download pre-built binaries from releases
```

### First-time Setup

```bash
# Run interactive setup wizard
xfer setup

# Or manually configure
xfer config set server https://xfer.example.com
```

### Send a File

```bash
xfer send photo.jpg

# With password protection
xfer send --password document.pdf

# Send multiple files (auto-zipped)
xfer send file1.txt file2.txt folder/

# Send a directory
xfer send my-folder/
```

### Receive Files

```bash
xfer receive

# Receive to specific directory
xfer receive ~/Downloads/
```

## How It Works

1. **Sender** runs `xfer send file.txt`
2. CLI generates encryption keys and displays a QR code
3. **Receiver** scans the QR code with their phone
4. File transfers encrypted through the relay server
5. Browser decrypts locally and downloads the file

```
Mobile Browser ──HTTPS/E2E──► Relay Server ◄──WSS/E2E── CLI Client
     (decrypt)                  (blind relay)            (encrypt)
```

## Security

- **AES-256-GCM** encryption with **HKDF-SHA256** key derivation
- **256-bit cryptographic tokens** for session security
- **SHA-256 checksums** verify file integrity
- **Zero-knowledge server** - cannot access file contents or metadata
- Optional **Argon2id password protection** for extra security

See [SECURITY.md](../SECURITY.md) for detailed security analysis.

## Configuration

Configuration file: `~/.config/xfer/config.yaml`

```yaml
server: "https://xfer.example.com"
timeout: 10m
output_dir: "."
progress: true
history: true
hide_filenames: false
```

### Commands

```bash
xfer send <files...>           # Send files
xfer receive [path]            # Receive files
xfer config <get|set|list>     # Manage configuration
xfer history [clear]           # View/manage transfer history
xfer setup                     # Interactive setup wizard
xfer info                      # Show current config
xfer test                      # Test server connectivity
xfer version                   # Show version
```

## Server Deployment

### Docker (Recommended)

```bash
docker run -d \
  -e XFER_BASE_URL=https://xfer.example.com \
  -p 127.0.0.1:8080:8080 \
  ghcr.io/user/xfer:server
```

### Docker Compose

```yaml
services:
  xfer:
    image: ghcr.io/user/xfer:server
    environment:
      XFER_BASE_URL: https://xfer.example.com
      XFER_PORT: 8080
      XFER_SESSION_TTL: 5m
      XFER_MAX_SIZE: 209715200  # 200MB
    ports:
      - "127.0.0.1:8080:8080"
    restart: unless-stopped
```

**Note:** Server requires HTTPS reverse proxy (nginx/Traefik/Caddy) for production.

## Architecture

See [ARCHITECTURE.md](../ARCHITECTURE.md) for technical details.

## Development

```bash
# Clone repository
git clone https://github.com/user/xfer.git
cd xfer

# Install dependencies
go mod download

# Build
go build ./cmd/xfer
go build ./cmd/xfer-server

# Run tests
go test ./...
```

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

Contributions welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) first.

## Acknowledgments

Inspired by [qrcp](https://github.com/claudiodangelis/qrcp) and similar tools, reimagined with security-first design.
