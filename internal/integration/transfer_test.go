package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xfer/internal/crypto"
	"xfer/internal/testutil"
)

func TestBrowserUploadToCLIReceive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name string
		size int64
	}{
		{"500KB", testutil.Size500KB},
		{"2MB", testutil.Size2MB},
		{"10MB", testutil.Size10MB},
		{"100MB", testutil.Size100MB},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testBrowserUploadSingleFile(t, tc.size)
		})
	}
}

func TestBrowserUploadMultiFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ts := testutil.NewTestServer(t)
	defer ts.Close()

	sourceDir := t.TempDir()
	outputDir := t.TempDir()

	files := []*testutil.TestFile{
		testutil.CreateTestFile(t, sourceDir, "small.bin", testutil.Size500KB),
		testutil.CreateTestFile(t, sourceDir, "medium.bin", testutil.Size2MB),
		testutil.CreateTestFile(t, sourceDir, "large.bin", testutil.Size10MB),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	masterKey, err := crypto.GenerateMasterKey()
	if err != nil {
		t.Fatalf("Failed to generate master key: %v", err)
	}

	session, err := createReceiveSession(ts.URL)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	wsURL := buildWSURL(ts.URL, session.Token)

	errChan := make(chan error, 1)
	go func() {
		errChan <- simulateCLIReceive(ctx, wsURL, masterKey, outputDir)
	}()

	time.Sleep(100 * time.Millisecond)

	uploader := testutil.NewBrowserUploader(wsURL, masterKey)
	for _, f := range files {
		uploader.AddFile(f)
	}

	if err := uploader.Upload(ctx); err != nil {
		t.Fatalf("Browser upload failed: %v", err)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("CLI receive failed: %v", err)
	}

	for _, f := range files {
		receivedPath := filepath.Join(outputDir, filepath.Base(f.Path))
		if !testutil.VerifyFile(t, receivedPath, f) {
			t.Errorf("File verification failed for %s", f.Path)
		}
	}
}

func TestCLISendToBrowserDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	tests := []struct {
		name string
		size int64
	}{
		{"500KB", testutil.Size500KB},
		{"2MB", testutil.Size2MB},
		{"10MB", testutil.Size10MB},
		{"100MB", testutil.Size100MB},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testCLISendSingleFile(t, tc.size)
		})
	}
}

func TestStressRepeatedUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	iterations := 5

	for i := 0; i < iterations; i++ {
		t.Run(fmt.Sprintf("iteration_%d", i), func(t *testing.T) {
			testBrowserUploadSingleFile(t, testutil.Size10MB)
		})
	}
}

func TestStressRepeatedDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	iterations := 5

	for i := 0; i < iterations; i++ {
		t.Run(fmt.Sprintf("iteration_%d", i), func(t *testing.T) {
			testCLISendSingleFile(t, testutil.Size10MB)
		})
	}
}

func testBrowserUploadSingleFile(t *testing.T, size int64) {
	t.Helper()

	ts := testutil.NewTestServer(t)
	defer ts.Close()

	sourceDir := t.TempDir()
	outputDir := t.TempDir()

	testFile := testutil.CreateTestFile(t, sourceDir, "testfile.bin", size)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	masterKey, err := crypto.GenerateMasterKey()
	if err != nil {
		t.Fatalf("Failed to generate master key: %v", err)
	}

	session, err := createReceiveSession(ts.URL)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	wsURL := buildWSURL(ts.URL, session.Token)

	errChan := make(chan error, 1)
	go func() {
		errChan <- simulateCLIReceive(ctx, wsURL, masterKey, outputDir)
	}()

	time.Sleep(100 * time.Millisecond)

	uploader := testutil.NewBrowserUploader(wsURL, masterKey)
	uploader.AddFile(testFile)

	if err := uploader.Upload(ctx); err != nil {
		t.Fatalf("Browser upload failed: %v", err)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("CLI receive failed: %v", err)
	}

	receivedPath := filepath.Join(outputDir, "testfile.bin")
	if !testutil.VerifyFile(t, receivedPath, testFile) {
		t.Error("File verification failed")
	}
}

func testCLISendSingleFile(t *testing.T, size int64) {
	t.Helper()

	ts := testutil.NewTestServer(t)
	defer ts.Close()

	sourceDir := t.TempDir()
	outputDir := t.TempDir()

	testFile := testutil.CreateTestFile(t, sourceDir, "testfile.bin", size)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	masterKey, err := crypto.GenerateMasterKey()
	if err != nil {
		t.Fatalf("Failed to generate master key: %v", err)
	}

	session, err := createSendSession(ts.URL)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	wsURL := buildWSURL(ts.URL, session.Token)

	errChan := make(chan error, 1)
	go func() {
		errChan <- simulateCLISend(ctx, wsURL, masterKey, testFile)
	}()

	time.Sleep(100 * time.Millisecond)

	downloader := testutil.NewBrowserDownloader(wsURL, masterKey, outputDir)
	result, err := downloader.Download(ctx)
	if err != nil {
		t.Fatalf("Browser download failed: %v", err)
	}

	if !result.Complete {
		t.Error("Download did not complete")
	}

	if err := <-errChan; err != nil {
		t.Fatalf("CLI send failed: %v", err)
	}

	if len(result.Files) != 1 {
		t.Fatalf("Expected 1 file, got %d", len(result.Files))
	}

	if result.Files[0].Checksum != testFile.Checksum {
		t.Errorf("Checksum mismatch: expected %s, got %s", testFile.Checksum, result.Files[0].Checksum)
	}
}

type sessionResponse struct {
	Token     string    `json:"token"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func createReceiveSession(serverURL string) (*sessionResponse, error) {
	reqBody := map[string]interface{}{
		"type":      "receive",
		"encrypted": true,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := http.Post(serverURL+"/api/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var session sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, err
	}

	return &session, nil
}

func createSendSession(serverURL string) (*sessionResponse, error) {
	reqBody := map[string]interface{}{
		"type":      "send",
		"encrypted": true,
	}
	body, _ := json.Marshal(reqBody)

	resp, err := http.Post(serverURL+"/api/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var session sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, err
	}

	return &session, nil
}

func buildWSURL(serverURL, token string) string {
	wsURL := serverURL + "/api/sessions/" + token + "/ws"
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)
	return wsURL
}

func simulateCLIReceive(ctx context.Context, wsURL string, masterKey []byte, outputDir string) error {
	return testutil.SimulateCLIReceive(ctx, wsURL, masterKey, outputDir)
}

func simulateCLISend(ctx context.Context, wsURL string, masterKey []byte, testFile *testutil.TestFile) error {
	return testutil.SimulateCLISend(ctx, wsURL, masterKey, testFile)
}
