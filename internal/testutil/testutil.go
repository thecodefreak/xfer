package testutil

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thecodefreak/xfer/internal/server"
)

type TestServer struct {
	URL        string
	Port       int
	httpServer *http.Server
	listener   net.Listener
}

func NewTestServer(t *testing.T) *TestServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", port)

	srv := server.New(&server.Config{
		Port:       fmt.Sprintf(":%d", port),
		BaseURL:    url,
		SessionTTL: 5 * time.Minute,
		MaxSize:    500 * 1024 * 1024, // 500MB for tests
	})

	httpServer := &http.Server{Handler: srv.Handler()}

	go func() {
		httpServer.Serve(listener)
	}()

	time.Sleep(50 * time.Millisecond)

	return &TestServer{
		URL:        url,
		Port:       port,
		httpServer: httpServer,
		listener:   listener,
	}
}

func (ts *TestServer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ts.httpServer.Shutdown(ctx)
	ts.listener.Close()
}

type TestFile struct {
	Path     string
	Size     int64
	Checksum string
}

func CreateTestFile(t *testing.T, dir string, name string, size int64) *TestFile {
	t.Helper()

	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	written := int64(0)
	buf := make([]byte, 64*1024)

	for written < size {
		toWrite := size - written
		if toWrite > int64(len(buf)) {
			toWrite = int64(len(buf))
		}

		_, err := rand.Read(buf[:toWrite])
		if err != nil {
			f.Close()
			t.Fatalf("Failed to generate random data: %v", err)
		}

		n, err := writer.Write(buf[:toWrite])
		if err != nil {
			f.Close()
			t.Fatalf("Failed to write test file: %v", err)
		}
		written += int64(n)
	}

	if err := f.Close(); err != nil {
		t.Fatalf("Failed to close test file: %v", err)
	}

	return &TestFile{
		Path:     path,
		Size:     size,
		Checksum: hex.EncodeToString(hasher.Sum(nil)),
	}
}

func VerifyFile(t *testing.T, path string, expected *TestFile) bool {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Errorf("Failed to stat received file: %v", err)
		return false
	}

	if info.Size() != expected.Size {
		t.Errorf("Size mismatch: got %d, expected %d", info.Size(), expected.Size)
		return false
	}

	checksum, err := CalculateChecksum(path)
	if err != nil {
		t.Errorf("Failed to calculate checksum: %v", err)
		return false
	}

	if checksum != expected.Checksum {
		t.Errorf("Checksum mismatch: got %s, expected %s", checksum, expected.Checksum)
		return false
	}

	return true
}

func CalculateChecksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

const (
	Size500KB = 500 * 1024
	Size2MB   = 2 * 1024 * 1024
	Size10MB  = 10 * 1024 * 1024
	Size100MB = 100 * 1024 * 1024
)
