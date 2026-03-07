package testutil

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/hkdf"
)

const chunkSize = 64 * 1024

type BrowserUploader struct {
	wsURL     string
	masterKey []byte
	files     []*TestFile
}

func NewBrowserUploader(wsURL string, masterKey []byte) *BrowserUploader {
	return &BrowserUploader{
		wsURL:     wsURL,
		masterKey: masterKey,
		files:     make([]*TestFile, 0),
	}
}

func (b *BrowserUploader) AddFile(f *TestFile) {
	b.files = append(b.files, f)
}

type uploadResult struct {
	Success bool
	Error   error
}

func (b *BrowserUploader) Upload(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, b.wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket connect failed: %w", err)
	}
	defer conn.Close()

	keys, err := deriveKeys(b.masterKey)
	if err != nil {
		return fmt.Errorf("key derivation failed: %w", err)
	}

	pendingAcks := make(map[int]chan struct{})
	var ackMu sync.Mutex
	errChan := make(chan error, 1)

	go func() {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(data, &msg) != nil {
				continue
			}

			if msg.Type == "file_acknowledged" {
				var ack struct {
					FileID int `json:"file_id"`
				}
				if json.Unmarshal(msg.Payload, &ack) == nil {
					ackMu.Lock()
					if ch, ok := pendingAcks[ack.FileID]; ok {
						close(ch)
						delete(pendingAcks, ack.FileID)
					}
					ackMu.Unlock()
				}
			}

			if msg.Type == "error" {
				var errPayload struct {
					Message string `json:"message"`
				}
				if json.Unmarshal(msg.Payload, &errPayload) == nil {
					select {
					case errChan <- fmt.Errorf("server error: %s", errPayload.Message):
					default:
					}
				}
			}
		}
	}()

	for fileID, tf := range b.files {
		file, err := os.Open(tf.Path)
		if err != nil {
			return fmt.Errorf("failed to open file %s: %w", tf.Path, err)
		}

		info, err := file.Stat()
		if err != nil {
			file.Close()
			return fmt.Errorf("failed to stat file: %w", err)
		}

		metadata := map[string]interface{}{
			"name":     info.Name(),
			"size":     info.Size(),
			"checksum": tf.Checksum,
		}
		metaJSON, _ := json.Marshal(metadata)
		encryptedMeta, err := encryptMetadata(metaJSON, keys.metaKey, keys.metaNonce)
		if err != nil {
			file.Close()
			return fmt.Errorf("metadata encryption failed: %w", err)
		}

		metaMsg := map[string]interface{}{
			"type": "metadata",
			"payload": map[string]interface{}{
				"encrypted_metadata":   encryptedMeta,
				"password_protected":   false,
				"file_id":              fileID,
				"expected_chunks":      0,
				"expected_relay_bytes": 0,
			},
		}
		metaMsgData, _ := json.Marshal(metaMsg)
		if err := conn.WriteMessage(websocket.TextMessage, metaMsgData); err != nil {
			file.Close()
			return fmt.Errorf("failed to send metadata: %w", err)
		}

		buf := make([]byte, chunkSize)
		var chunkIndex int
		var relayBytes int64

		for {
			n, readErr := file.Read(buf)
			if n > 0 {
				ciphertext, err := encryptChunk(buf[:n], keys.fileKey, keys.fileNonce, uint64(chunkIndex))
				if err != nil {
					file.Close()
					return fmt.Errorf("chunk encryption failed: %w", err)
				}

				frame := make([]byte, 4+len(ciphertext))
				binary.BigEndian.PutUint32(frame[:4], uint32(len(ciphertext)))
				copy(frame[4:], ciphertext)

				if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
					file.Close()
					return fmt.Errorf("failed to send chunk: %w", err)
				}

				relayBytes += int64(len(frame))
				chunkIndex++
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				file.Close()
				return fmt.Errorf("file read error: %w", readErr)
			}
		}
		file.Close()

		fileEndMsg := map[string]interface{}{
			"type": "file_end",
			"payload": map[string]interface{}{
				"file_id":              fileID,
				"expected_chunks":      chunkIndex,
				"expected_relay_bytes": relayBytes,
			},
		}
		fileEndData, _ := json.Marshal(fileEndMsg)
		if err := conn.WriteMessage(websocket.TextMessage, fileEndData); err != nil {
			return fmt.Errorf("failed to send file_end: %w", err)
		}

		ackCh := make(chan struct{})
		ackMu.Lock()
		pendingAcks[fileID] = ackCh
		ackMu.Unlock()

		select {
		case <-ackCh:
		case err := <-errChan:
			return err
		case <-time.After(45 * time.Second):
			return fmt.Errorf("timeout waiting for file acknowledgment")
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	completeMsg := map[string]interface{}{
		"type":    "complete",
		"payload": map[string]interface{}{},
	}
	completeData, _ := json.Marshal(completeMsg)
	if err := conn.WriteMessage(websocket.TextMessage, completeData); err != nil {
		return fmt.Errorf("failed to send complete: %w", err)
	}

	time.Sleep(100 * time.Millisecond)

	return nil
}

type BrowserDownloader struct {
	wsURL     string
	masterKey []byte
	outputDir string
}

func NewBrowserDownloader(wsURL string, masterKey []byte, outputDir string) *BrowserDownloader {
	return &BrowserDownloader{
		wsURL:     wsURL,
		masterKey: masterKey,
		outputDir: outputDir,
	}
}

type DownloadResult struct {
	Files    []*DownloadedFile
	Error    error
	Complete bool
}

type DownloadedFile struct {
	Name     string
	Size     int64
	Checksum string
	Path     string
}

func (b *BrowserDownloader) Download(ctx context.Context) (*DownloadResult, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, b.wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket connect failed: %w", err)
	}
	defer conn.Close()

	keys, err := deriveKeys(b.masterKey)
	if err != nil {
		return nil, fmt.Errorf("key derivation failed: %w", err)
	}

	result := &DownloadResult{
		Files: make([]*DownloadedFile, 0),
	}

	var currentFile *os.File
	var currentMeta struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		Checksum string `json:"checksum"`
	}
	var currentPath string
	var receivedSize int64
	var chunkCounter uint64
	var chunks [][]byte

	finalizeCurrent := func() error {
		if currentFile == nil {
			return nil
		}

		for _, chunk := range chunks {
			if _, err := currentFile.Write(chunk); err != nil {
				return fmt.Errorf("failed to write chunk: %w", err)
			}
		}

		if err := currentFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync: %w", err)
		}
		if err := currentFile.Close(); err != nil {
			return fmt.Errorf("failed to close: %w", err)
		}

		actualChecksum, err := CalculateChecksum(currentPath)
		if err != nil {
			return fmt.Errorf("checksum calculation failed: %w", err)
		}

		if actualChecksum != currentMeta.Checksum {
			return fmt.Errorf("checksum mismatch: expected %s, got %s", currentMeta.Checksum, actualChecksum)
		}

		result.Files = append(result.Files, &DownloadedFile{
			Name:     currentMeta.Name,
			Size:     currentMeta.Size,
			Checksum: currentMeta.Checksum,
			Path:     currentPath,
		})

		currentFile = nil
		currentPath = ""
		receivedSize = 0
		chunkCounter = 0
		chunks = nil
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				if currentFile != nil && receivedSize >= currentMeta.Size {
					if err := finalizeCurrent(); err != nil {
						return result, err
					}
				}
				result.Complete = true
				return result, nil
			}
			if currentFile != nil && receivedSize > 0 {
				return result, fmt.Errorf("transfer incomplete: received %d of %d bytes", receivedSize, currentMeta.Size)
			}
			return result, fmt.Errorf("read error: %w", err)
		}

		if messageType == websocket.TextMessage {
			var msg struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(data, &msg) != nil {
				continue
			}

			switch msg.Type {
			case "status":
				continue

			case "metadata":
				if currentFile != nil && receivedSize >= currentMeta.Size {
					if err := finalizeCurrent(); err != nil {
						return result, err
					}
				}

				var payload struct {
					EncryptedMeta []byte `json:"encrypted_metadata"`
				}
				if json.Unmarshal(msg.Payload, &payload) != nil {
					return result, fmt.Errorf("invalid metadata payload")
				}

				metaJSON, err := decryptMetadata(payload.EncryptedMeta, keys.metaKey, keys.metaNonce)
				if err != nil {
					return result, fmt.Errorf("metadata decryption failed: %w", err)
				}

				if json.Unmarshal(metaJSON, &currentMeta) != nil {
					return result, fmt.Errorf("invalid metadata JSON")
				}

				currentPath = b.outputDir + "/" + currentMeta.Name
				currentFile, err = os.Create(currentPath)
				if err != nil {
					return result, fmt.Errorf("failed to create file: %w", err)
				}
				receivedSize = 0
				chunkCounter = 0
				chunks = nil

			case "complete":
				if currentFile != nil && receivedSize >= currentMeta.Size {
					if err := finalizeCurrent(); err != nil {
						return result, err
					}
				}
				result.Complete = true
				return result, nil
			}
			continue
		}

		if currentFile == nil {
			continue
		}

		if len(data) < 4 {
			continue
		}

		chunkLen := binary.BigEndian.Uint32(data[:4])
		if int(chunkLen)+4 > len(data) {
			return result, fmt.Errorf("invalid chunk length")
		}
		ciphertext := data[4 : 4+chunkLen]

		plaintext, err := decryptChunk(ciphertext, keys.fileKey, keys.fileNonce, chunkCounter)
		if err != nil {
			return result, fmt.Errorf("decryption failed: %w", err)
		}

		chunks = append(chunks, plaintext)
		receivedSize += int64(len(plaintext))
		chunkCounter++

		if receivedSize >= currentMeta.Size {
			if err := finalizeCurrent(); err != nil {
				return result, err
			}
		}
	}
}

type derivedKeys struct {
	fileKey   []byte
	metaKey   []byte
	fileNonce []byte
	metaNonce []byte
}

func deriveKeys(masterKey []byte) (*derivedKeys, error) {
	keys := &derivedKeys{}

	fileKeyReader := hkdf.New(sha256.New, masterKey, nil, []byte("xfer-file"))
	keys.fileKey = make([]byte, 32)
	if _, err := io.ReadFull(fileKeyReader, keys.fileKey); err != nil {
		return nil, err
	}

	metaKeyReader := hkdf.New(sha256.New, masterKey, nil, []byte("xfer-metadata"))
	keys.metaKey = make([]byte, 32)
	if _, err := io.ReadFull(metaKeyReader, keys.metaKey); err != nil {
		return nil, err
	}

	fileNonceReader := hkdf.New(sha256.New, masterKey, nil, []byte("xfer-file-nonce"))
	keys.fileNonce = make([]byte, 4)
	if _, err := io.ReadFull(fileNonceReader, keys.fileNonce); err != nil {
		return nil, err
	}

	metaNonceReader := hkdf.New(sha256.New, masterKey, nil, []byte("xfer-meta-nonce"))
	keys.metaNonce = make([]byte, 4)
	if _, err := io.ReadFull(metaNonceReader, keys.metaNonce); err != nil {
		return nil, err
	}

	return keys, nil
}

func makeNonce(prefix []byte, counter uint64) []byte {
	nonce := make([]byte, 12)
	copy(nonce[:4], prefix)
	binary.BigEndian.PutUint64(nonce[4:], counter)
	return nonce
}

func encryptChunk(data []byte, key []byte, noncePrefix []byte, counter uint64) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := makeNonce(noncePrefix, counter)
	return aesGCM.Seal(nil, nonce, data, nil), nil
}

func decryptChunk(ciphertext []byte, key []byte, noncePrefix []byte, counter uint64) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := makeNonce(noncePrefix, counter)
	return aesGCM.Open(nil, nonce, ciphertext, nil)
}

func encryptMetadata(data []byte, key []byte, noncePrefix []byte) ([]byte, error) {
	return encryptChunk(data, key, noncePrefix, 0)
}

func decryptMetadata(ciphertext []byte, key []byte, noncePrefix []byte) ([]byte, error) {
	return decryptChunk(ciphertext, key, noncePrefix, 0)
}
