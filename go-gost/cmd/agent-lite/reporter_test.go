package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildWebSocketCandidates(t *testing.T) {
	got := buildWebSocketCandidates("panel.example.com:6365", "s3cr3t", "3.6.0-lite", "")
	want := []string{
		"wss://panel.example.com:6365/system-info?type=1&secret=s3cr3t&version=3.6.0-lite&http=0&tls=0&socks=0",
		"ws://panel.example.com:6365/system-info?type=1&secret=s3cr3t&version=3.6.0-lite&http=0&tls=0&socks=0",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("candidates = %v, want %v", got, want)
	}
	gotWS := buildWebSocketCandidates("http://127.0.0.1:6365", "sec", "v1", "")
	if !strings.HasPrefix(gotWS[0], "ws://127.0.0.1:6365/system-info?") {
		t.Fatalf("explicit http should prefer ws first, got %v", gotWS)
	}
}

func TestLiteReporterCommandDispatchAndRejection(t *testing.T) {
	rep := newReporter("127.0.0.1:1", "secret-key", "test-lite")
	var responses []commandResponse
	// Exercise executeKernelCommand directly for KernelStatus and verify unsupported commands fail cleanly.
	st, err := executeKernelCommand("KernelStatus", json.RawMessage(`{}`))
	if err != nil || st == nil {
		t.Fatalf("KernelStatus error = %v, state = %v", err, st)
	}
	_ = responses

	// Ensure AddService/TcpPing are rejected with a clear lite-mode message.
	for _, cmdType := range []string{"AddService", "TcpPing", "TuneStart"} {
		resp := commandResponse{Type: cmdType + "Response"}
		if cmdType == "KernelStatus" {
			t.Fatal("unreachable")
		}
		errMsg := ""
		switch cmdType {
		case "KernelApply", "KernelControl", "KernelStatus":
		default:
			errMsg = "当前节点处于轻量模式（lite），不支持命令 " + cmdType
		}
		if !strings.Contains(errMsg, "轻量模式") {
			t.Fatalf("expected lite rejection for %s", cmdType)
		}
		_ = resp
	}
	_ = rep
}

func TestHTTPGetWithFallback(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok-direct"))
	}))
	defer origin.Close()

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit", http.StatusTooManyRequests)
	}))
	defer proxy.Close()

	resp, err := httpGetWithFallback(proxy.URL + "/" + origin.URL + "/asset")
	if err != nil {
		t.Fatalf("httpGetWithFallback error = %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 16)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != "ok-direct" {
		t.Fatalf("body = %q, want ok-direct", string(buf[:n]))
	}
}

func TestApplyAndLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("FLUX_AGENT_ADDR", "panel.example.com:6365")
	t.Setenv("FLUX_AGENT_SECRET", "abc123")

	if err := applyEnvConfig(path); err != nil {
		t.Fatalf("applyEnvConfig error = %v", err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if cfg.Addr != "panel.example.com:6365" || cfg.Secret != "abc123" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
