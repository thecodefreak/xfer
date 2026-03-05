package client

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
	"xfer/internal/crypto"
	"xfer/internal/protocol"
)

type ReceiveOptions struct {
	OutputDir    string
	ServerURL    string
	Insecure     bool
	Timeout      time.Duration
	ShowProgress bool
}

func Receive(ctx context.Context, opts ReceiveOptions) error {
	client := NewClient(opts.ServerURL, opts.Insecure, opts.Timeout)

	fmt.Println("Connecting to server...")
	session, err := client.CreateReceiveSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	conn, err := client.ConnectWebSocket(ctx, session.WebSocketURL)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.CloseConn(conn)

	stopChan := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = client.CloseConn(conn)
		case <-stopChan:
		}
	}()
	defer close(stopChan)

	masterKey, err := crypto.GenerateMasterKey()
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	keyStr := crypto.EncodeKey(masterKey)
	uploadURL := session.UploadURL + "#k=" + keyStr

	fmt.Println("\nScan QR code to upload:")
	printQRCode(uploadURL)
	fmt.Printf("\n%s\n\n", uploadURL)

	fmt.Println("Waiting for sender...")

	var msg *protocol.Message
	for {
		msg, err = client.ReadMessage(conn)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("connection error: %w", err)
		}
		if msg == nil {
			continue
		}
		if msg.Type == protocol.MessageTypeStatus {
			continue
		}
		if msg.Type == protocol.MessageTypeMetadata {
			break
		}
	}

	fmt.Println("Sender connected!")

	payloadBytes, _ := json.Marshal(msg.Payload)
	var metaData struct {
		EncryptedMeta      []byte `json:"encrypted_metadata"`
		PasswordProtected  bool   `json:"password_protected"`
		EncryptedMasterKey []byte `json:"encrypted_master_key"`
		Salt               []byte `json:"salt"`
	}
	json.Unmarshal(payloadBytes, &metaData)

	var derivedKeys *crypto.DerivedKeys
	if metaData.PasswordProtected {
		fmt.Print("Enter password: ")
		var password string
		fmt.Scanln(&password)

		masterKey, err = crypto.DecryptMasterKey(metaData.EncryptedMasterKey, password, metaData.Salt)
		if err != nil {
			return fmt.Errorf("invalid password: %w", err)
		}
	}

	derivedKeys, err = crypto.DeriveKeys(masterKey)
	if err != nil {
		return fmt.Errorf("failed to derive keys: %w", err)
	}

	metaJSON, err := crypto.DecryptMetadata(metaData.EncryptedMeta, derivedKeys.MetadataKey, derivedKeys.MetaNonce)
	if err != nil {
		return fmt.Errorf("failed to decrypt metadata: %w", err)
	}

	var fileMeta struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		Checksum string `json:"checksum"`
	}
	json.Unmarshal(metaJSON, &fileMeta)

	fmt.Printf("Receiving: %s (%d bytes)\n", fileMeta.Name, fileMeta.Size)

	outputPath := filepath.Join(opts.OutputDir, fileMeta.Name)
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	var receivedSize int64 = 0
	var counter uint64 = 0
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) || websocket.IsUnexpectedCloseError(err) {
				if receivedSize >= fileMeta.Size {
					break
				}
			}
			return fmt.Errorf("failed to read data: %w", err)
		}

		if messageType == websocket.TextMessage {
			var msg protocol.Message
			if json.Unmarshal(data, &msg) == nil && msg.Type == protocol.MessageTypeComplete {
				break
			}
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

		plaintext, err := crypto.DecryptChunk(ciphertext, derivedKeys.FileKey, derivedKeys.FileNonce, counter)
		if err != nil {
			return fmt.Errorf("decryption failed: %w", err)
		}

		if _, err := file.Write(plaintext); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		receivedSize += int64(len(plaintext))
		counter++

		if receivedSize >= fileMeta.Size {
			break
		}
	}

	file.Sync()

	actualChecksum, err := crypto.CalculateChecksum(outputPath)
	if err != nil {
		return fmt.Errorf("failed to verify checksum: %w", err)
	}
	if actualChecksum != fileMeta.Checksum {
		os.Remove(outputPath)
		return fmt.Errorf("checksum mismatch: expected %s, got %s", fileMeta.Checksum, actualChecksum)
	}

	fmt.Printf("\nTransfer complete! Saved to: %s\n", outputPath)
	return nil
}
