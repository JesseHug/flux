package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-gost/x/kernel"
	"github.com/gorilla/websocket"
)

const (
	reporterReadWait  = 60 * time.Second
	reporterWriteWait = 5 * time.Second
	wsPingInterval    = 20 * time.Second
	initialBackoff    = 2 * time.Second
	maxBackoff        = 2 * time.Minute
	maxCommandWorkers = 4
)

type commandMessage struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	RequestId string          `json:"requestId,omitempty"`
}

type commandResponse struct {
	Type      string      `json:"type"`
	Success   bool        `json:"success"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
	RequestId string      `json:"requestId,omitempty"`
}

type liteReporter struct {
	addr              string
	secret            string
	version           string
	preferredWSScheme string

	ctx       context.Context
	cancel    context.CancelFunc
	aesCrypto *aesCrypto

	connMutex sync.Mutex
	conn      *websocket.Conn
	connected bool

	curBackoff time.Duration
	cmdSem     chan struct{}
	workers    sync.WaitGroup
}

var wsDial = func(dialer *websocket.Dialer, rawURL string) (*websocket.Conn, *http.Response, error) {
	return dialer.Dial(rawURL, nil)
}

func newReporter(addr, secret, version string) *liteReporter {
	ctx, cancel := context.WithCancel(context.Background())
	c, err := newAESCrypto(secret)
	if err != nil {
		fmt.Printf("❌ 创建 AES 加密器失败: %v\n", err)
	}
	return &liteReporter{
		addr:       addr,
		secret:     secret,
		version:    version,
		ctx:        ctx,
		cancel:     cancel,
		aesCrypto:  c,
		curBackoff: initialBackoff,
		cmdSem:     make(chan struct{}, maxCommandWorkers),
	}
}

func (r *liteReporter) Start() {
	r.workers.Add(1)
	go func() {
		defer r.workers.Done()
		r.run()
	}()
}

func (r *liteReporter) Stop() {
	r.cancel()
	r.connMutex.Lock()
	if r.conn != nil {
		_ = r.conn.Close()
	}
	r.connMutex.Unlock()
	r.workers.Wait()
}

func (r *liteReporter) run() {
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}
		if err := r.connect(); err != nil {
			wait := time.Duration(float64(r.curBackoff) * (0.75 + rand.Float64()*0.5))
			fmt.Printf("❌ WebSocket连接失败: %v，%v后重试\n", err, wait)
			if r.curBackoff *= 2; r.curBackoff > maxBackoff {
				r.curBackoff = maxBackoff
			}
			select {
			case <-time.After(wait):
			case <-r.ctx.Done():
				return
			}
			continue
		}
		r.curBackoff = initialBackoff
		r.serveConn()
	}
}

func (r *liteReporter) connect() error {
	candidates := buildWebSocketCandidates(r.addr, r.secret, r.version, r.preferredWSScheme)
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	var lastErr error
	for _, target := range candidates {
		conn, resp, err := wsDial(&dialer, target)
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if err != nil {
			lastErr = err
			continue
		}
		r.connMutex.Lock()
		r.conn = conn
		r.connected = true
		r.connMutex.Unlock()
		if strings.HasPrefix(target, "ws://") {
			r.preferredWSScheme = "ws"
		} else if strings.HasPrefix(target, "wss://") {
			r.preferredWSScheme = "wss"
		}
		_ = conn.SetReadDeadline(time.Now().Add(reporterReadWait))
		conn.SetPingHandler(func(appData string) error {
			_ = conn.SetReadDeadline(time.Now().Add(reporterReadWait))
			return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(reporterWriteWait))
		})
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(reporterReadWait))
		})
		fmt.Printf("✅ 轻量模式 WebSocket 连接成功\n")
		return nil
	}
	if lastErr == nil {
		return fmt.Errorf("WebSocket 候选地址为空")
	}
	return lastErr
}

func buildWebSocketCandidates(addr, secret, version, preferredScheme string) []string {
	raw := strings.TrimSpace(addr)
	explicitScheme := ""
	if idx := strings.Index(raw, "://"); idx > 0 {
		explicitScheme = strings.ToLower(strings.TrimSpace(raw[:idx]))
		if parsed, err := url.Parse(raw); err == nil && strings.TrimSpace(parsed.Host) != "" {
			raw = strings.TrimSpace(parsed.Host)
		} else {
			raw = raw[idx+3:]
		}
	}
	if idx := strings.IndexAny(raw, "/?#"); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	query := "/system-info?type=1&secret=" + url.QueryEscape(secret) +
		"&version=" + url.QueryEscape(version) + "&http=0&tls=0&socks=0"
	schemes := []string{"wss", "ws"}
	if explicitScheme == "ws" || explicitScheme == "http" || (explicitScheme == "" && preferredScheme == "ws") {
		schemes = []string{"ws", "wss"}
	}
	return []string{
		schemes[0] + "://" + raw + query,
		schemes[1] + "://" + raw + query,
	}
}

func (r *liteReporter) serveConn() {
	r.connMutex.Lock()
	conn := r.conn
	r.connMutex.Unlock()
	if conn == nil {
		return
	}

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			select {
			case <-r.ctx.Done():
				return
			default:
			}
			msgType, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				r.handleTextMessage(payload)
			}
		}
	}()

	pingTicker := time.NewTicker(wsPingInterval)
	defer func() {
		pingTicker.Stop()
		r.connMutex.Lock()
		r.connected = false
		if r.conn == conn {
			r.conn = nil
		}
		r.connMutex.Unlock()
		_ = conn.Close()
		<-readDone
	}()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-readDone:
			return
		case <-pingTicker.C:
			r.connMutex.Lock()
			c := r.conn
			ok := r.connected
			r.connMutex.Unlock()
			if !ok || c == nil {
				return
			}
			if err := c.WriteControl(websocket.PingMessage, nil, time.Now().Add(reporterWriteWait)); err != nil {
				return
			}
		}
	}
}

func (r *liteReporter) handleTextMessage(message []byte) {
	var wrapper struct {
		Encrypted bool   `json:"encrypted"`
		Data      string `json:"data"`
	}
	if json.Unmarshal(message, &wrapper) == nil && wrapper.Encrypted {
		if r.aesCrypto == nil {
			r.sendResponse(commandResponse{Type: "NoDecryptor", Success: false, Message: "没有可用的解密器"})
			return
		}
		dec, err := r.aesCrypto.Decrypt(wrapper.Data)
		if err != nil {
			r.sendResponse(commandResponse{Type: "DecryptError", Success: false, Message: fmt.Sprintf("解密失败: %v", err)})
			return
		}
		message = dec
	}

	var compressed struct {
		Type       string          `json:"type"`
		Compressed bool            `json:"compressed"`
		Data       json.RawMessage `json:"data"`
		RequestId  string          `json:"requestId,omitempty"`
	}
	var cmd commandMessage
	if json.Unmarshal(message, &compressed) == nil && compressed.Compressed {
		gr, err := gzip.NewReader(bytes.NewReader(compressed.Data))
		if err != nil {
			r.sendResponse(commandResponse{Type: "DecompressError", Success: false, Message: err.Error(), RequestId: compressed.RequestId})
			return
		}
		decompressed, err := io.ReadAll(io.LimitReader(gr, 32<<20))
		_ = gr.Close()
		if err != nil {
			r.sendResponse(commandResponse{Type: "DecompressError", Success: false, Message: err.Error(), RequestId: compressed.RequestId})
			return
		}
		cmd = commandMessage{Type: compressed.Type, Data: decompressed, RequestId: compressed.RequestId}
	} else if err := json.Unmarshal(message, &cmd); err != nil {
		r.sendResponse(commandResponse{Type: "ParseError", Success: false, Message: fmt.Sprintf("解析命令失败: %v", err)})
		return
	}

	if cmd.Type == "" || cmd.Type == "call" {
		return
	}
	select {
	case r.cmdSem <- struct{}{}:
		go func() {
			defer func() {
				<-r.cmdSem
				if rec := recover(); rec != nil {
					r.sendResponse(commandResponse{
						Type:      cmd.Type + "Response",
						Success:   false,
						Message:   fmt.Sprintf("命令处理异常: %v", rec),
						RequestId: cmd.RequestId,
					})
				}
			}()
			r.executeCommand(cmd)
		}()
	case <-r.ctx.Done():
	default:
		r.sendResponse(commandResponse{
			Type:      cmd.Type + "Response",
			Success:   false,
			Message:   "轻量节点命令并发已满，请稍后重试",
			RequestId: cmd.RequestId,
		})
	}
}

func (r *liteReporter) executeCommand(cmd commandMessage) {
	resp := commandResponse{
		Type:      cmd.Type + "Response",
		RequestId: cmd.RequestId,
	}
	var (
		data interface{}
		err  error
	)
	switch cmd.Type {
	case "KernelApply", "KernelControl", "KernelStatus":
		data, err = executeKernelCommand(cmd.Type, cmd.Data)
		debug.FreeOSMemory()
	case "UpgradeAgent":
		err = r.handleUpgradeAgent(cmd.Data)
	case "RollbackAgent":
		err = r.handleRollbackAgent()
	case "UninstallAgent":
		err = r.handleUninstallAgent()
	default:
		err = fmt.Errorf("当前节点处于轻量模式（lite），不支持命令 %s", cmd.Type)
	}
	resp.Data = data
	if err != nil {
		resp.Success = false
		resp.Message = err.Error()
	} else {
		resp.Success = true
		resp.Message = "OK"
	}
	r.sendResponse(resp)
}

func executeKernelCommand(cmdType string, raw json.RawMessage) (interface{}, error) {
	m := kernel.Default()
	switch cmdType {
	case "KernelApply":
		var req kernel.ApplyRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("解析内核配置失败: %w", err)
		}
		return m.Apply(req)
	case "KernelControl":
		var req struct {
			Type   string `json:"type"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, fmt.Errorf("解析内核操作失败: %w", err)
		}
		return m.Control(req.Type, req.Action)
	case "KernelStatus":
		var req struct {
			Logs string `json:"logs"`
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &req)
		}
		return m.Status(req.Logs), nil
	default:
		return nil, fmt.Errorf("未知内核命令: %s", cmdType)
	}
}

func (r *liteReporter) sendResponse(resp commandResponse) {
	payload, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if r.aesCrypto != nil {
		if enc, err := r.aesCrypto.Encrypt(payload); err == nil {
			if wrapped, err := json.Marshal(map[string]interface{}{
				"encrypted": true,
				"data":      enc,
				"timestamp": time.Now().Unix(),
			}); err == nil {
				payload = wrapped
			}
		}
	}
	r.connMutex.Lock()
	defer r.connMutex.Unlock()
	if r.conn == nil || !r.connected {
		return
	}
	timeout := reporterWriteWait
	if len(payload) > 1024*1024 {
		timeout = 30 * time.Second
	}
	_ = r.conn.SetWriteDeadline(time.Now().Add(timeout))
	if err := r.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		r.connected = false
	}
}

func (r *liteReporter) sendUpgradeProgress(stage string, percent int, message string) {
	payload, err := json.Marshal(map[string]interface{}{
		"type":      "UpgradeProgress",
		"stage":     stage,
		"percent":   percent,
		"message":   message,
		"timestamp": time.Now().UnixMilli(),
	})
	if err != nil {
		return
	}
	if r.aesCrypto != nil {
		if enc, err := r.aesCrypto.Encrypt(payload); err == nil {
			if wrapped, err := json.Marshal(map[string]interface{}{
				"encrypted": true,
				"data":      enc,
				"timestamp": time.Now().Unix(),
			}); err == nil {
				payload = wrapped
			}
		}
	}
	r.connMutex.Lock()
	defer r.connMutex.Unlock()
	if r.conn == nil || !r.connected {
		return
	}
	_ = r.conn.SetWriteDeadline(time.Now().Add(reporterWriteWait))
	_ = r.conn.WriteMessage(websocket.TextMessage, payload)
}
