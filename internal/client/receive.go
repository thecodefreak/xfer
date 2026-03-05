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

	spinner := NewSpinner("Waiting for sender...")
	spinnerDone := make(chan struct{})
	spinnerActive := true

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-spinnerDone:
				return
			case <-ticker.C:
				fmt.Print(spinner.Tick())
			}
		}
	}()

	stopSpinner := func() {
		if spinnerActive {
			close(spinnerDone)
			fmt.Print(spinner.Clear())
			spinnerActive = false
		}
	}

	derivedKeys, err := crypto.DeriveKeys(masterKey)
	if err != nil {
		stopSpinner()
		return fmt.Errorf("failed to derive keys: %w", err)
	}

	var currentFile *os.File
	var currentPath string
	var receivedSize int64
	var counter uint64
	var receivedAny bool
	var fileIndex int
	var fileMeta struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		Checksum string `json:"checksum"`
	}

	progress := NewProgress(opts.ShowProgress)

	finalizeCurrent := func() error {
		if currentFile == nil {
			return nil
		}
		if receivedSize != fileMeta.Size {
			return fmt.Errorf("transfer incomplete for %s", fileMeta.Name)
		}
		if err := currentFile.Sync(); err != nil {
			return fmt.Errorf("failed to sync file: %w", err)
		}
		if err := currentFile.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}

		actualChecksum, err := crypto.CalculateChecksum(currentPath)
		if err != nil {
			return fmt.Errorf("failed to verify checksum: %w", err)
		}
		if actualChecksum != fileMeta.Checksum {
			os.Remove(currentPath)
			return fmt.Errorf("checksum mismatch: expected %s, got %s", fileMeta.Checksum, actualChecksum)
		}

		receivedAny = true
		progress.Complete()
		if opts.ShowProgress {
			fmt.Print(progress.Render())
		}
		currentFile = nil
		currentPath = ""
		receivedSize = 0
		counter = 0
		return nil
	}

	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			stopSpinner()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) || websocket.IsUnexpectedCloseError(err) {
				if currentFile != nil {
					if err := finalizeCurrent(); err != nil {
						return err
					}
				}
				break
			}
			return fmt.Errorf("failed to read data: %w", err)
		}

		if messageType == websocket.TextMessage {
			var msg protocol.Message
			if json.Unmarshal(data, &msg) != nil {
				continue
			}

			switch msg.Type {
			case protocol.MessageTypeStatus:
				continue
			case protocol.MessageTypeComplete:
				if currentFile != nil {
					if receivedSize < fileMeta.Size {
						return fmt.Errorf("transfer incomplete for %s", fileMeta.Name)
					}
					if err := finalizeCurrent(); err != nil {
						return err
					}
				}
				fmt.Printf("\nTransfer complete! Files saved to: %s\n", opts.OutputDir)
				return nil
			case protocol.MessageTypeMetadata:
				stopSpinner()

				if currentFile != nil {
					if receivedSize < fileMeta.Size {
						return fmt.Errorf("received new file before finishing %s", fileMeta.Name)
					}
					if err := finalizeCurrent(); err != nil {
						return err
					}
				}

				payloadBytes, _ := json.Marshal(msg.Payload)
				var metaData struct {
					EncryptedMeta      []byte `json:"encrypted_metadata"`
					PasswordProtected  bool   `json:"password_protected"`
					EncryptedMasterKey []byte `json:"encrypted_master_key"`
					Salt               []byte `json:"salt"`
				}
				json.Unmarshal(payloadBytes, &metaData)

				if metaData.PasswordProtected {
					fmt.Print("Enter password: ")
					var password string
					fmt.Scanln(&password)

					masterKey, err = crypto.DecryptMasterKey(metaData.EncryptedMasterKey, password, metaData.Salt)
					if err != nil {
						return fmt.Errorf("invalid password: %w", err)
					}
					derivedKeys, err = crypto.DeriveKeys(masterKey)
					if err != nil {
						return fmt.Errorf("failed to derive keys: %w", err)
					}
				}

				metaJSON, err := crypto.DecryptMetadata(metaData.EncryptedMeta, derivedKeys.MetadataKey, derivedKeys.MetaNonce)
				if err != nil {
					return fmt.Errorf("failed to decrypt metadata: %w", err)
				}

				json.Unmarshal(metaJSON, &fileMeta)

				if opts.ShowProgress {
					fmt.Println()
				}
				progress.SetFile(fileMeta.Name, fileIndex, 1, fileMeta.Size)
				fileIndex++

				currentPath = filepath.Join(opts.OutputDir, fileMeta.Name)
				currentFile, err = os.Create(currentPath)
				if err != nil {
					return fmt.Errorf("failed to create file: %w", err)
				}
				receivedSize = 0
				counter = 0
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

		plaintext, err := crypto.DecryptChunk(ciphertext, derivedKeys.FileKey, derivedKeys.FileNonce, counter)
		if err != nil {
			return fmt.Errorf("decryption failed: %w", err)
		}

		if _, err := currentFile.Write(plaintext); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		receivedSize += int64(len(plaintext))
		counter++

		progress.Update(int64(len(plaintext)))
		if opts.ShowProgress {
			fmt.Print(progress.Render())
		}

		if receivedSize >= fileMeta.Size {
			if err := finalizeCurrent(); err != nil {
				return err
			}
		}
	}

	if currentFile != nil {
		if err := finalizeCurrent(); err != nil {
			return err
		}
	}

	if !receivedAny {
		return fmt.Errorf("transfer incomplete")
	}

	fmt.Printf("\nTransfer complete! Files saved to: %s\n", opts.OutputDir)
	return nil
}
