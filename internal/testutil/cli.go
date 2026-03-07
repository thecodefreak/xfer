package testutil

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
)

func SimulateCLIReceive(ctx context.Context, wsURL string, masterKey []byte, outputDir string) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket connect failed: %w", err)
	}
	defer conn.Close()

	keys, err := deriveKeys(masterKey)
	if err != nil {
		return fmt.Errorf("key derivation failed: %w", err)
	}

	var currentFile *os.File
	var currentPath string
	var receivedSize int64
	var counter uint64
	var fileCommitted bool
	var completeReceived bool
	var currentFileID int
	var fileMeta struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		Checksum string `json:"checksum"`
	}

	finalizeCurrent := func() error {
		if currentFile == nil {
			return nil
		}
		if receivedSize != fileMeta.Size {
			return fmt.Errorf("incomplete: received %d of %d bytes", receivedSize, fileMeta.Size)
		}
		if err := currentFile.Sync(); err != nil {
			return err
		}
		if err := currentFile.Close(); err != nil {
			return err
		}

		actualChecksum, err := CalculateChecksum(currentPath)
		if err != nil {
			return fmt.Errorf("checksum calculation failed: %w", err)
		}
		if actualChecksum != fileMeta.Checksum {
			os.Remove(currentPath)
			return fmt.Errorf("checksum mismatch")
		}

		currentFile = nil
		currentPath = ""
		receivedSize = 0
		counter = 0
		fileCommitted = false
		return nil
	}

	sendAck := func(fileID int) error {
		ack := map[string]interface{}{
			"type": "file_received_ack",
			"payload": map[string]interface{}{
				"file_id": fileID,
			},
		}
		data, _ := json.Marshal(ack)
		return conn.WriteMessage(websocket.TextMessage, data)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		readTimeout := 2 * time.Minute
		if completeReceived {
			readTimeout = 3 * time.Second
		}

		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return err
		}

		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if completeReceived && currentFile == nil {
				return nil
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				if currentFile != nil {
					return finalizeCurrent()
				}
				return nil
			}
			if completeReceived && currentFile != nil {
				return fmt.Errorf("incomplete: received %d of %d bytes", receivedSize, fileMeta.Size)
			}
			return fmt.Errorf("read error: %w", err)
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

			case "complete":
				completeReceived = true
				if currentFile != nil {
					if fileCommitted && receivedSize >= fileMeta.Size {
						if err := finalizeCurrent(); err != nil {
							return err
						}
						return nil
					}
					continue
				}
				return nil

			case "metadata":
				if currentFile != nil {
					if receivedSize < fileMeta.Size {
						return fmt.Errorf("new file before finishing previous")
					}
					if err := finalizeCurrent(); err != nil {
						return err
					}
				}

				var payload struct {
					EncryptedMeta []byte `json:"encrypted_metadata"`
					FileID        int    `json:"file_id"`
				}
				if json.Unmarshal(msg.Payload, &payload) != nil {
					return fmt.Errorf("invalid metadata payload")
				}

				metaJSON, err := decryptMetadata(payload.EncryptedMeta, keys.metaKey, keys.metaNonce)
				if err != nil {
					return fmt.Errorf("metadata decryption failed: %w", err)
				}

				if json.Unmarshal(metaJSON, &fileMeta) != nil {
					return fmt.Errorf("invalid metadata JSON")
				}

				currentFileID = payload.FileID
				currentPath = filepath.Join(outputDir, fileMeta.Name)
				currentFile, err = os.Create(currentPath)
				if err != nil {
					return fmt.Errorf("failed to create file: %w", err)
				}
				receivedSize = 0
				counter = 0
				fileCommitted = false

			case "file_committed":
				var payload struct {
					FileID int `json:"file_id"`
				}
				if json.Unmarshal(msg.Payload, &payload) != nil {
					return fmt.Errorf("invalid file_committed payload")
				}

				if currentFile == nil || payload.FileID != currentFileID {
					continue
				}

				fileCommitted = true

				if receivedSize >= fileMeta.Size {
					if err := finalizeCurrent(); err != nil {
						return err
					}
					if err := sendAck(payload.FileID); err != nil {
						return fmt.Errorf("failed to send ack: %w", err)
					}
				}
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
			return fmt.Errorf("invalid chunk length")
		}
		ciphertext := data[4 : 4+chunkLen]

		plaintext, err := decryptChunk(ciphertext, keys.fileKey, keys.fileNonce, counter)
		if err != nil {
			return fmt.Errorf("decryption failed: %w", err)
		}

		if _, err := currentFile.Write(plaintext); err != nil {
			return fmt.Errorf("write failed: %w", err)
		}
		receivedSize += int64(len(plaintext))
		counter++

		if fileCommitted && receivedSize >= fileMeta.Size {
			if err := finalizeCurrent(); err != nil {
				return err
			}
			if err := sendAck(currentFileID); err != nil {
				return fmt.Errorf("failed to send ack: %w", err)
			}
		}
	}
}

func SimulateCLISend(ctx context.Context, wsURL string, masterKey []byte, testFile *TestFile) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("websocket connect failed: %w", err)
	}
	defer conn.Close()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		_, data, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("waiting for browser: %w", err)
		}

		var msg struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		if msg.Type == "status" {
			var status struct {
				State string `json:"state"`
			}
			if json.Unmarshal(msg.Payload, &status) == nil && status.State == "active" {
				break
			}
		}
	}

	keys, err := deriveKeys(masterKey)
	if err != nil {
		return fmt.Errorf("key derivation failed: %w", err)
	}

	file, err := os.Open(testFile.Path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	metadata := map[string]interface{}{
		"name":     info.Name(),
		"size":     info.Size(),
		"checksum": testFile.Checksum,
	}
	metaJSON, _ := json.Marshal(metadata)
	encryptedMeta, err := encryptMetadata(metaJSON, keys.metaKey, keys.metaNonce)
	if err != nil {
		return fmt.Errorf("metadata encryption failed: %w", err)
	}

	metaMsg := map[string]interface{}{
		"type": "metadata",
		"payload": map[string]interface{}{
			"encrypted_metadata": encryptedMeta,
		},
	}
	metaMsgData, _ := json.Marshal(metaMsg)
	if err := conn.WriteMessage(websocket.TextMessage, metaMsgData); err != nil {
		return fmt.Errorf("failed to send metadata: %w", err)
	}

	buf := make([]byte, chunkSize)
	var counter uint64

	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			ciphertext, err := encryptChunk(buf[:n], keys.fileKey, keys.fileNonce, counter)
			if err != nil {
				return fmt.Errorf("chunk encryption failed: %w", err)
			}

			frame := make([]byte, 4+len(ciphertext))
			binary.BigEndian.PutUint32(frame[:4], uint32(len(ciphertext)))
			copy(frame[4:], ciphertext)

			if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return fmt.Errorf("failed to send chunk: %w", err)
			}
			counter++
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("file read error: %w", readErr)
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

	time.Sleep(500 * time.Millisecond)

	return nil
}
