package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/thecodefreak/xfer/internal/protocol"
)

const (
	pingInterval     = 30 * time.Second
	pongWait         = 60 * time.Second
	controlWriteWait = 10 * time.Second
	binaryWriteWait  = 90 * time.Second
	bufferSize       = 64 * 1024
	maxMessageSize   = 256 * 1024
)

type Client struct {
	serverURL  string
	insecure   bool
	timeout    time.Duration
	httpClient *http.Client
	wsDialer   *websocket.Dialer
	connStates sync.Map
}

type connState struct {
	mu   sync.Mutex
	done chan struct{}
	once sync.Once
}

type SendSession struct {
	Token        string
	URL          string
	DownloadURL  string
	WebSocketURL string
	ExpiresAt    time.Time
}

type ReceiveSession struct {
	Token        string
	URL          string
	UploadURL    string
	WebSocketURL string
	ExpiresAt    time.Time
}

func NewClient(serverURL string, insecure bool, timeout time.Duration) *Client {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}

	return &Client{
		serverURL: strings.TrimRight(serverURL, "/"),
		insecure:  insecure,
		timeout:   timeout,
		httpClient: &http.Client{
			Transport: tr,
			Timeout:   timeout,
		},
		wsDialer: &websocket.Dialer{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
		},
	}
}

func (c *Client) CreateSendSession() (*SendSession, error) {
	reqBody := protocol.SessionRequest{
		Type:      string(protocol.SessionTypeSend),
		Encrypted: true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.serverURL+"/api/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var sessionResp protocol.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	downloadURL := c.serverURL + "/download/" + sessionResp.Token
	wsURL := c.serverURL + "/api/sessions/" + sessionResp.Token + "/ws"
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)

	return &SendSession{
		Token:        sessionResp.Token,
		URL:          sessionResp.URL,
		DownloadURL:  downloadURL,
		WebSocketURL: wsURL,
		ExpiresAt:    sessionResp.ExpiresAt,
	}, nil
}

func (c *Client) CreateReceiveSession() (*ReceiveSession, error) {
	reqBody := protocol.SessionRequest{
		Type:      string(protocol.SessionTypeReceive),
		Encrypted: true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.serverURL+"/api/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var sessionResp protocol.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	uploadURL := c.serverURL + "/upload/" + sessionResp.Token
	wsURL := c.serverURL + "/api/sessions/" + sessionResp.Token + "/ws"
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.Replace(wsURL, "https://", "wss://", 1)

	return &ReceiveSession{
		Token:        sessionResp.Token,
		URL:          sessionResp.URL,
		UploadURL:    uploadURL,
		WebSocketURL: wsURL,
		ExpiresAt:    sessionResp.ExpiresAt,
	}, nil
}

func (c *Client) ConnectWebSocket(ctx context.Context, wsURL string) (*websocket.Conn, error) {
	headers := http.Header{}
	headers.Set("Origin", c.serverURL)

	conn, _, err := c.wsDialer.DialContext(ctx, wsURL, headers)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to WebSocket: %w", err)
	}

	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	state := &connState{done: make(chan struct{})}
	c.connStates.Store(conn, state)

	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-state.done:
				return
			case <-ticker.C:
				state.mu.Lock()
				conn.SetWriteDeadline(time.Now().Add(controlWriteWait))
				err := conn.WriteMessage(websocket.PingMessage, nil)
				state.mu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	return conn, nil
}

func (c *Client) SendMessage(conn *websocket.Conn, msg protocol.Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}
	state := c.getConnState(conn)
	state.mu.Lock()
	defer state.mu.Unlock()
	conn.SetWriteDeadline(time.Now().Add(controlWriteWait))
	return conn.WriteMessage(websocket.TextMessage, data)
}

func (c *Client) SendBinary(conn *websocket.Conn, data []byte) error {
	state := c.getConnState(conn)
	state.mu.Lock()
	defer state.mu.Unlock()
	conn.SetWriteDeadline(time.Now().Add(binaryWriteWait))
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func (c *Client) ReadMessage(conn *websocket.Conn) (*protocol.Message, error) {
	typ, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	if typ == websocket.PingMessage {
		conn.WriteMessage(websocket.PongMessage, nil)
		return nil, nil
	}

	var msg protocol.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return &msg, nil
}

func (c *Client) ReadBinary(conn *websocket.Conn) ([]byte, error) {
	typ, data, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}

	if typ == websocket.PingMessage {
		conn.WriteMessage(websocket.PongMessage, nil)
		return nil, nil
	}

	return data, nil
}

func (c *Client) CloseConn(conn *websocket.Conn) error {
	state := c.getConnState(conn)
	state.once.Do(func() { close(state.done) })

	state.mu.Lock()
	conn.SetWriteDeadline(time.Now().Add(controlWriteWait))
	err := conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	state.mu.Unlock()

	c.connStates.Delete(conn)
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *Client) getConnState(conn *websocket.Conn) *connState {
	if state, ok := c.connStates.Load(conn); ok {
		return state.(*connState)
	}
	state := &connState{done: make(chan struct{})}
	c.connStates.Store(conn, state)
	return state
}

func (c *Client) ServerURL() string {
	return c.serverURL
}

func buildURL(base, path string, params map[string]string) string {
	u, _ := url.Parse(base + path)
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String()
}
