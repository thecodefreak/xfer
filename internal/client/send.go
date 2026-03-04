package client

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/gorilla/websocket"
	qrcode "github.com/skip2/go-qrcode"
	"xfer/internal/crypto"
	"xfer/internal/protocol"
)

type SendOptions struct {
	Files        []string
	ServerURL    string
	Insecure     bool
	Timeout      time.Duration
	ShowProgress bool
	Password     string
}

func Send(ctx context.Context, opts SendOptions) error {
	if len(opts.Files) == 0 {
		return fmt.Errorf("no files provided")
	}

	var totalSize int64

	for _, path := range opts.Files {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("cannot access file %s: %w", path, err)
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
	}

	useZip := false
	for _, path := range opts.Files {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("cannot access %s: %w", path, err)
		}
		if info.IsDir() {
			useZip = true
		}
	}

	if len(opts.Files) > 1 {
		useZip = true
	}

	var zipPath string
	if useZip {
		var err error
		zipPath, totalSize, err = createZipFile(opts.Files)
		if err != nil {
			return fmt.Errorf("failed to create zip: %w", err)
		}
		defer os.Remove(zipPath)
		opts.Files = []string{zipPath}
	}

	client := NewClient(opts.ServerURL, opts.Insecure, opts.Timeout)

	fmt.Println("Connecting to server...")
	session, err := client.CreateSendSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	conn, err := client.ConnectWebSocket(ctx, session.WebSocketURL)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.CloseConn(conn)

	masterKey, err := crypto.GenerateMasterKey()
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	keyStr := crypto.EncodeKey(masterKey)

	derivedKeys, err := crypto.DeriveKeys(masterKey)
	if err != nil {
		return fmt.Errorf("failed to derive keys: %w", err)
	}

	var encryptedMasterKey []byte
	var salt []byte
	if opts.Password != "" {
		salt, err = crypto.GenerateSalt()
		if err != nil {
			return fmt.Errorf("failed to generate salt: %w", err)
		}
		encryptedMasterKey, err = crypto.EncryptMasterKey(masterKey, opts.Password, salt)
		if err != nil {
			return fmt.Errorf("failed to encrypt master key: %w", err)
		}
	}

	downloadURL := session.DownloadURL + "#k=" + keyStr
	if opts.Password != "" {
		downloadURL += "&p=1"
	}

	metadata := protocol.FileMetadata{
		PasswordProtected: opts.Password != "",
	}
	if opts.Password != "" {
		metadata.EncryptedMasterKey = encryptedMasterKey
		metadata.Salt = salt
	}

	_ = metadata

	fmt.Println("\nScan QR code to download:")
	printQRCode(downloadURL)
	fmt.Printf("\n%s\n\n", downloadURL)

	fmt.Println("Waiting for receiver...")
	msg, err := client.ReadMessage(conn)
	if err != nil {
		return fmt.Errorf("connection error: %w", err)
	}
	if msg == nil {
		return fmt.Errorf("unexpected nil message")
	}

	if msg.Type != protocol.MessageTypeStatus {
		return fmt.Errorf("unexpected message type: %s", msg.Type)
	}

	var status protocol.TransferStatus
	payloadBytes, _ := json.Marshal(msg.Payload)
	json.Unmarshal(payloadBytes, &status)

	if status.State != protocol.StateActive {
		return fmt.Errorf("session not active: %s", status.Message)
	}

	fmt.Println("Receiver connected!")

	for _, path := range opts.Files {
		if err := sendFile(client, conn, path, derivedKeys); err != nil {
			return fmt.Errorf("failed to send %s: %w", path, err)
		}
	}

	if err := client.SendMessage(conn, protocol.Message{Type: protocol.MessageTypeComplete}); err != nil {
		return fmt.Errorf("failed to send completion: %w", err)
	}

	fmt.Println("\nTransfer complete!")
	return nil
}

func sendFile(client *Client, conn *websocket.Conn, path string, keys *crypto.DerivedKeys) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	checksum, err := crypto.CalculateChecksum(path)
	if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	meta := struct {
		Name     string `json:"name"`
		Size     int64  `json:"size"`
		Checksum string `json:"checksum"`
	}{
		Name:     info.Name(),
		Size:     info.Size(),
		Checksum: checksum,
	}

	metaJSON, _ := json.Marshal(meta)
	encryptedMeta, err := crypto.EncryptMetadata(metaJSON, keys.MetadataKey, keys.MetaNonce)
	if err != nil {
		return fmt.Errorf("failed to encrypt metadata: %w", err)
	}

	metaPayload := map[string]interface{}{
		"encrypted_metadata": encryptedMeta,
	}
	if err := client.SendMessage(conn, protocol.Message{Type: protocol.MessageTypeMetadata, Payload: metaPayload}); err != nil {
		return fmt.Errorf("failed to send metadata: %w", err)
	}

	buf := make([]byte, crypto.ChunkSize)
	var counter uint64 = 0

	for {
		n, err := file.Read(buf)
		if n > 0 {
			ciphertext, err := crypto.EncryptChunk(buf[:n], keys.FileKey, keys.FileNonce, counter)
			if err != nil {
				return fmt.Errorf("encryption failed: %w", err)
			}

			frame := make([]byte, 4+len(ciphertext))
			binary.BigEndian.PutUint32(frame[:4], uint32(len(ciphertext)))
			copy(frame[4:], ciphertext)

			if err := client.SendBinary(conn, frame); err != nil {
				return fmt.Errorf("failed to send data: %w", err)
			}
			counter++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
	}

	return nil
}

func printQRCode(url string) {
	qr, err := qrcode.New(url, qrcode.Medium)
	if err != nil {
		fmt.Printf("(Could not generate QR code: %v)\n", err)
		return
	}
	fmt.Print(qr.ToSmallString(false))
}

func createZipFile(files []string) (string, int64, error) {
	tmpFile, err := os.CreateTemp("", "xfer-*.zip")
	if err != nil {
		return "", 0, err
	}
	defer tmpFile.Close()

	zipWriter := zip.NewWriter(tmpFile)
	defer zipWriter.Close()

	for _, path := range files {
		if err := addFileToZip(zipWriter, path); err != nil {
			os.Remove(tmpFile.Name())
			return "", 0, err
		}
	}

	if err := zipWriter.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return "", 0, err
	}

	info, err := os.Stat(tmpFile.Name())
	if err != nil {
		os.Remove(tmpFile.Name())
		return "", 0, err
	}

	return tmpFile.Name(), info.Size(), nil
}

func addFileToZip(zipWriter *zip.Writer, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if info.IsDir() {
		return filepath.Walk(path, func(filePath string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.IsDir() {
				return nil
			}
			relPath, _ := filepath.Rel(filepath.Dir(path), filePath)
			return addSingleFileToZip(zipWriter, filePath, relPath)
		})
	}

	return addSingleFileToZip(zipWriter, path, filepath.Base(path))
}

func addSingleFileToZip(zipWriter *zip.Writer, sourcePath, targetPath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = targetPath
	header.Method = zip.Deflate

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, file)
	return err
}
