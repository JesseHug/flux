package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go-backend/internal/security"
	"go-backend/internal/store/model"
	"go-backend/internal/store/repo"
	"go-backend/internal/ws"
)

func setupLiveKernelStatusAgent(t *testing.T, r *repo.Repository, nodeID int64, secret string, responder func(cmd cleanupAgentCommand) (bool, interface{})) *ws.Server {
	t.Helper()
	server := ws.NewServer(r, "test-jwt-secret")
	online := make(chan struct{})
	server.SetNodeOnlineHook(func(int64) {
		select {
		case <-online:
		default:
			close(online)
		}
	})
	serverDone := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer close(serverDone)
		server.ServeHTTP(w, req)
	}))
	t.Cleanup(ts.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"?type=1&secret="+secret, nil)
	if err != nil {
		t.Fatal(err)
	}
	crypto, err := security.NewAESCrypto(secret)
	if err != nil {
		t.Fatal(err)
	}

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var envelope struct {
				Encrypted bool   `json:"encrypted"`
				Data      string `json:"data"`
			}
			if err := json.Unmarshal(payload, &envelope); err != nil {
				return
			}
			if envelope.Encrypted {
				payload, err = crypto.Decrypt(envelope.Data)
				if err != nil {
					return
				}
			}
			var command cleanupAgentCommand
			if err := json.Unmarshal(payload, &command); err != nil {
				return
			}
			ok, data := responder(command)
			resp := map[string]interface{}{
				"type":      command.Type,
				"requestId": command.RequestID,
				"success":   ok,
				"data":      data,
			}
			if err := conn.WriteJSON(resp); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = conn.Close()
		<-readerDone
		<-serverDone
	})

	select {
	case <-online:
	case <-time.After(3 * time.Second):
		t.Fatal("mock node did not come online")
	}

	h := &Handler{repo: r, wsServer: server}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := h.sendNodeCommandWithTimeout(nodeID, "KernelStatus", map[string]interface{}{}, time.Second, false, false); err == nil {
			return server
		}
		if time.Now().After(deadline) {
			t.Fatal("agent session not registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSubscriptionResetPollsOnlineNodesBeforeResetting(t *testing.T) {
	r, err := repo.Open(filepath.Join(t.TempDir(), "reset-poll.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	const secret = "reset-poll-secret"
	now := time.Now().UnixMilli()
	if err := r.DB().Create(&model.Node{ID: 1, Name: "n1", Secret: secret, ServerIP: "127.0.0.1", Port: "30000", Status: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(1, 'xray', 'latest', 'running', ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	kernelID := mustLastInsertID(t, r, "kernel")
	if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, settings, created_time, updated_time)
		VALUES(?, 1, 'vless', 'vless', '::', 443, '{"uuid":"00000000-0000-4000-8000-000000000000"}', ?, ?)`, kernelID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	s := &model.Subscription{
		Name: "u", Kind: model.SubscriptionShare, Token: strings.Repeat("a", 32),
		UUID: "11111111-1111-4111-8111-111111111111", Password: "pw",
		SourceAll: 1, Enabled: 1, TrafficLimit: 5000, CreatedTime: now, UpdatedTime: now, TrafficSince: now,
	}
	if err := r.SaveSubscription(s); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	nodeDownBytes := int64(1000)

	wsSrv := setupLiveKernelStatusAgent(t, r, 1, secret, func(cmd cleanupAgentCommand) (bool, interface{}) {
		if cmd.Type == "KernelStatus" {
			mu.Lock()
			b := nodeDownBytes
			mu.Unlock()
			return true, map[string]interface{}{
				"kernels":      []interface{}{},
				"nftAvailable": true,
				"epoch":        "e1",
				"users":        []map[string]interface{}{{"id": s.ID, "dir": "down", "bytes": b}},
			}
		}
		return true, map[string]interface{}{}
	})
	h := &Handler{repo: r, wsServer: wsSrv}

	// Poll 1: node records 1000 bytes.
	h.pollNodeKernels(1, false)
	got, _ := r.GetSubscription(s.ID)
	if got.DownloadBytes != 1000 {
		t.Fatalf("after first poll: got %d, want 1000", got.DownloadBytes)
	}

	// User consumes 500 more bytes on node before the next 60s poll, then admin clicks Reset.
	mu.Lock()
	nodeDownBytes = 1500
	mu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscription/reset-traffic", bytes.NewBufferString(`{"id":`+strconv.FormatInt(s.ID, 10)+`}`))
	rec := httptest.NewRecorder()
	h.subscriptionResetTraffic(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status: %d %s", rec.Code, rec.Body.String())
	}

	// Next poll sees the same 1500 cumulative bytes: 0 must leak into the new cycle!
	h.pollNodeKernels(1, false)
	got, _ = r.GetSubscription(s.ID)
	if got.DownloadBytes != 0 {
		t.Fatalf("pre-reset bytes leaked after manual reset: got %d, want 0", got.DownloadBytes)
	}

	// Same protection must hold for monthly ResetDay auto-resets in syncSubscriptions.
	monthStart := time.Date(2026, 4, 1, 0, 5, 0, 0, time.Local)
	s.ResetDay = 1
	s.TrafficResetTime = time.Date(2026, 3, 1, 0, 5, 0, 0, time.Local).UnixMilli()
	_ = r.DB().Model(&model.Subscription{}).Where("id = ?", s.ID).Updates(map[string]interface{}{
		"reset_day": s.ResetDay, "traffic_reset_time": s.TrafficResetTime,
	}).Error
	mu.Lock()
	nodeDownBytes = 2200
	mu.Unlock()
	h.syncSubscriptions(monthStart)
	h.pollNodeKernels(1, false)
	got, _ = r.GetSubscription(s.ID)
	if got.DownloadBytes != 0 {
		t.Fatalf("pre-reset bytes leaked after monthly ResetDay reset: got %d, want 0", got.DownloadBytes)
	}
}

func TestKernelStatsCapablePollAndRenderEvenWithoutNft(t *testing.T) {
	r, err := repo.Open(filepath.Join(t.TempDir(), "stats-capable.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	const secret = "stats-capable-secret"
	now := time.Now().UnixMilli()
	if err := r.DB().Create(&model.Node{ID: 1, Name: "lxc", Secret: secret, ServerIP: "127.0.0.1", Port: "30000", Status: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(1, 'xray', 'latest', 'running', ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	kernelID := mustLastInsertID(t, r, "kernel")
	if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, settings, created_time, updated_time)
		VALUES(?, 1, 'vless', 'vless', '::', 443, '{"uuid":"00000000-0000-4000-8000-000000000000"}', ?, ?)`, kernelID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	s := &model.Subscription{
		Name: "lxc-user", Kind: model.SubscriptionShare, Token: strings.Repeat("c", 32),
		UUID: "22222222-2222-4222-8222-222222222222", Password: "pw",
		SourceAll: 1, Enabled: 1, CreatedTime: now, UpdatedTime: now, TrafficSince: now,
	}
	if err := r.SaveSubscription(s); err != nil {
		t.Fatal(err)
	}

	wsSrv := setupLiveKernelStatusAgent(t, r, 1, secret, func(cmd cleanupAgentCommand) (bool, interface{}) {
		if cmd.Type == "KernelStatus" {
			return true, map[string]interface{}{
				"kernels":      []interface{}{},
				"nftAvailable": false,
				"nftError":     "Operation not permitted",
				"statsCapable": true,
				"statsEpoch":   "s1",
				"statsUsers": []map[string]interface{}{
					{"id": s.ID, "dir": "up", "bytes": 400},
					{"id": s.ID, "dir": "down", "bytes": 1600},
				},
			}
		}
		return true, map[string]interface{}{}
	})
	h := &Handler{repo: r, wsServer: wsSrv}

	// Even with nftAvailable=false (unprivileged container), StatsUsers is recorded
	// and node.StatsCapable is persisted so Xray payload enables StatsService.
	h.pollNodeKernels(1, false)
	got, _ := r.GetSubscription(s.ID)
	if got.UploadBytes != 400 || got.DownloadBytes != 1600 {
		t.Fatalf("expected stats traffic 400/1600 without nftables, got %d/%d", got.UploadBytes, got.DownloadBytes)
	}
	node, _ := r.GetNodeByID(1)
	if node.StatsCapable != 1 {
		t.Fatalf("expected node.StatsCapable=1, got %d", node.StatsCapable)
	}
	// TestKernelStatsCapablePollAndRenderEvenWithoutNft verifies that when a
	// container node lacks nftables permissions (nftCannotMark), Xray switches
	// to StatsService while nodes with nftables permissions stay on nft.
	k, _ := r.GetNodeKernel(kernelID)
	payload, _, err := h.buildKernelApplyPayload(k)
	if err != nil {
		t.Fatal(err)
	}
	if payload["statsAddr"] != "127.0.0.1:19850" || payload["userMarks"] != nil {
		t.Fatalf("expected Xray payload to use StatsService without nft marks: statsAddr=%v userMarks=%v", payload["statsAddr"], payload["userMarks"])
	}
}

func TestDualKernelNormalNodeUsesNftForBothXrayAndSingbox(t *testing.T) {
	r, err := repo.Open(filepath.Join(t.TempDir(), "dual-normal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	now := time.Now().UnixMilli()
	if err := r.DB().Create(&model.Node{ID: 1, Name: "kvm", Secret: "s", ServerIP: "127.0.0.1", Port: "30000", Status: 1, StatsCapable: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(1, 'xray', 'latest', 'running', ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	xrayID := mustLastInsertID(t, r, "kernel")
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(1, 'singbox', 'latest', 'running', ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	singboxID := mustLastInsertID(t, r, "kernel")
	if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, settings, created_time, updated_time)
		VALUES(?, 1, 'xray-vless', 'vless', '::', 443, '{"uuid":"00000000-0000-4000-8000-000000000000"}', ?, ?)`, xrayID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, settings, security, created_time, updated_time)
		VALUES(?, 1, 'sb-hy2', 'hysteria2', '::', 8443, '{"password":"owner"}', '{"type":"tls","sni":"a.example.com","certPem":"c","keyPem":"k"}', ?, ?)`, singboxID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	s := &model.Subscription{
		Name: "kvm-sub", Kind: model.SubscriptionShare, Token: strings.Repeat("e", 32),
		UUID: "44444444-4444-4444-8444-444444444444", Password: "pw",
		SourceAll: 1, Enabled: 1, CreatedTime: now, UpdatedTime: now, TrafficSince: now,
	}
	if err := r.SaveSubscription(s); err != nil {
		t.Fatal(err)
	}
	h := New(r, "jwt-secret")
	for _, kid := range []int64{xrayID, singboxID} {
		k, _ := r.GetNodeKernel(kid)
		p, _, err := h.buildKernelApplyPayload(k)
		if err != nil {
			t.Fatal(err)
		}
		if p["statsAddr"] != nil || p["userMarks"] == nil {
			t.Fatalf("normal node kernel %s must stay on nft userMarks without statsAddr: statsAddr=%v userMarks=%v", k.KernelType, p["statsAddr"], p["userMarks"])
		}
	}
}

func TestDualSourceStatsAndNftCountersAggregateWithoutCollision(t *testing.T) {
	r, err := repo.Open(filepath.Join(t.TempDir(), "dual-kernel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	const secret = "dual-kernel-secret"
	now := time.Now().UnixMilli()
	if err := r.DB().Create(&model.Node{ID: 1, Name: "dual", Secret: secret, ServerIP: "127.0.0.1", Port: "30000", Status: 1, StatsCapable: 1, NftError: "Operation not permitted"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(1, 'xray', 'latest', 'running', ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	xrayID := mustLastInsertID(t, r, "kernel")
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(1, 'singbox', 'latest', 'running', ?, ?)`, now, now).Error; err != nil {
		t.Fatal(err)
	}
	singboxID := mustLastInsertID(t, r, "kernel")
	if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, settings, created_time, updated_time)
		VALUES(?, 1, 'xray-vless', 'vless', '::', 443, '{"uuid":"00000000-0000-4000-8000-000000000000"}', ?, ?)`, xrayID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Exec(`INSERT INTO node_inbound(kernel_id, node_id, name, protocol, listen, port, settings, security, created_time, updated_time)
		VALUES(?, 1, 'sb-hy2', 'hysteria2', '::', 8443, '{"password":"owner"}', '{"type":"tls","sni":"a.example.com","certPem":"c","keyPem":"k"}', ?, ?)`, singboxID, now, now).Error; err != nil {
		t.Fatal(err)
	}

	s := &model.Subscription{
		Name: "dual-sub", Kind: model.SubscriptionShare, Token: strings.Repeat("d", 32),
		UUID: "33333333-3333-4333-8333-333333333333", Password: "pw",
		SourceAll: 1, Enabled: 1, CreatedTime: now, UpdatedTime: now, TrafficSince: now,
	}
	if err := r.SaveSubscription(s); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	nftEpoch, statsEpoch := "e1", "s1"
	nftUp, nftDown := int64(500), int64(1500)
	statsUp, statsDown := int64(1000), int64(2000)

	wsSrv := setupLiveKernelStatusAgent(t, r, 1, secret, func(cmd cleanupAgentCommand) (bool, interface{}) {
		if cmd.Type == "KernelStatus" {
			mu.Lock()
			defer mu.Unlock()
			return true, map[string]interface{}{
				"kernels":      []interface{}{},
				"nftAvailable": true,
				"epoch":        nftEpoch,
				"users": []map[string]interface{}{
					{"id": s.ID, "dir": "up", "bytes": nftUp},
					{"id": s.ID, "dir": "down", "bytes": nftDown},
				},
				"statsCapable": true,
				"statsEpoch":   statsEpoch,
				"statsUsers": []map[string]interface{}{
					{"id": s.ID, "dir": "up", "bytes": statsUp},
					{"id": s.ID, "dir": "down", "bytes": statsDown},
				},
			}
		}
		return true, map[string]interface{}{}
	})
	h := &Handler{repo: r, wsServer: wsSrv}

	// Poll 1: StatsUsers (1000/2000) + nft Users (500/1500) = 1500 up / 3500 down.
	h.pollNodeKernels(1, false)
	got, _ := r.GetSubscription(s.ID)
	if got.UploadBytes != 1500 || got.DownloadBytes != 3500 {
		t.Fatalf("poll 1 want 1500/3500, got %d/%d", got.UploadBytes, got.DownloadBytes)
	}

	// Poll 2: agent restarts StatsEpoch (s1 -> s2: 100/200) while nft keeps e1 (+300 down = 1800).
	mu.Lock()
	statsEpoch = "s2"
	statsUp, statsDown = 100, 200
	nftDown = 1800
	mu.Unlock()
	h.pollNodeKernels(1, false)
	got, _ = r.GetSubscription(s.ID)
	if got.UploadBytes != 1600 || got.DownloadBytes != 4000 {
		t.Fatalf("poll 2 want 1600/4000, got %d/%d", got.UploadBytes, got.DownloadBytes)
	}

	// Unpolled traffic arrives on both sources (+250 stats down, +150 nft down), then admin resets.
	mu.Lock()
	statsDown = 450
	nftDown = 1950
	mu.Unlock()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/subscription/reset-traffic", bytes.NewBufferString(`{"id":`+strconv.FormatInt(s.ID, 10)+`}`))
	rec := httptest.NewRecorder()
	h.subscriptionResetTraffic(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reset status: %d %s", rec.Code, rec.Body.String())
	}

	// Next poll with unchanged counters must stay 0/0 (zero pre-reset leakage).
	h.pollNodeKernels(1, false)
	got, _ = r.GetSubscription(s.ID)
	if got.UploadBytes != 0 || got.DownloadBytes != 0 {
		t.Fatalf("after reset want 0/0, got %d/%d", got.UploadBytes, got.DownloadBytes)
	}
}
