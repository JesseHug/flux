package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
)

func newLiteModeTestAgent(t *testing.T) *cleanupAgent {
	t.Helper()
	agent := newCleanupAgent(t)
	wsSrv := agent.h.wsServer
	agent.h = New(agent.h.repo, "test-jwt-secret")
	agent.h.wsServer = wsSrv
	return agent
}

func TestLiteModeNodeInstallAndUpgradeBinary(t *testing.T) {
	agent := newLiteModeTestAgent(t)
	if err := agent.h.repo.DB().Model(&model.Node{}).Where("id = ?", 1).Update("forward_mode", "lite").Error; err != nil {
		t.Fatal(err)
	}
	if err := agent.h.repo.DB().Create(&model.ViteConfig{Name: "ip", Value: "panel.example.com:6365"}).Error; err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"3.6.0","prerelease":false,"draft":false}]`))
	}))
	defer ts.Close()
	oldAPIBase := releaseAPIBase
	releaseAPIBase = ts.URL
	releaseCacheMu.Lock()
	releaseCache = nil
	releaseCacheAt = time.Time{}
	releaseCacheMu.Unlock()
	t.Cleanup(func() {
		releaseAPIBase = oldAPIBase
		releaseCacheMu.Lock()
		releaseCache = nil
		releaseCacheAt = time.Time{}
		releaseCacheMu.Unlock()
	})

	rec := httptest.NewRecorder()
	agent.h.nodeInstall(rec, httptest.NewRequest(http.MethodPost, "/api/v1/node/install", strings.NewReader(`{"id":1,"mode":"script"}`)))
	if !strings.Contains(rec.Body.String(), "AGENT_MODE=lite") {
		t.Fatalf("expected AGENT_MODE=lite in install command, got: %s", rec.Body.String())
	}

	upRec := httptest.NewRecorder()
	agent.h.nodeUpgrade(upRec, httptest.NewRequest(http.MethodPost, "/api/v1/node/upgrade", strings.NewReader(`{"id":1,"version":"3.6.0"}`)))
	cmds := agent.commandsOfType("UpgradeAgent")
	if len(cmds) == 0 {
		t.Fatalf("expected UpgradeAgent command, response=%s", upRec.Body.String())
	}
	var payload struct {
		DownloadURL string `json:"downloadUrl"`
		ChecksumURL string `json:"checksumUrl"`
	}
	if err := json.Unmarshal(cmds[len(cmds)-1].Data, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.DownloadURL, "gost-lite-{ARCH}") || !strings.Contains(payload.ChecksumURL, "gost-lite-{ARCH}.sha256") {
		t.Fatalf("unexpected upgrade payload for lite node: %+v", payload)
	}
}

func TestPanelPreresolvesGitHubKernelDownload(t *testing.T) {
	wantDigest := strings.Repeat("a", 64)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/SagerNet/sing-box/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v1.14.4","assets":[{"name":"sing-box-1.14.4-linux-amd64-musl.tar.gz","digest":"sha256:` + wantDigest + `","browser_download_url":"https://github.com/SagerNet/sing-box/releases/download/v1.14.4/sing-box-1.14.4-linux-amd64-musl.tar.gz"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	oldBase := kernelGitHubAPIBase
	kernelGitHubAPIBase = ts.URL
	kernelReleaseAssetCache.Lock()
	kernelReleaseAssetCache.at = map[string]time.Time{}
	kernelReleaseAssetCache.items = map[string]kernelReleaseDetail{}
	kernelReleaseAssetCache.Unlock()
	t.Cleanup(func() {
		kernelGitHubAPIBase = oldBase
	})

	info, ok := kernel.Lookup(kernel.TypeSingbox)
	if !ok {
		t.Fatal("singbox kernel info missing")
	}
	raw, err := kernel.ResolveDownload(info, "latest", "")
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolveGitHubKernelDownload(info, "latest", "", raw)
	gotURL, _ := resolved["url"].(string)
	if strings.Contains(gotURL, "{VERSION}") || !strings.Contains(gotURL, "1.14.4") {
		t.Fatalf("expected concrete version url, got %q", gotURL)
	}
	sums, _ := resolved["sha256"].(map[string]string)
	if sums["amd64"] != wantDigest {
		t.Fatalf("expected pre-resolved amd64 sha256 %q, got %+v", wantDigest, sums)
	}
}

func TestLiteModeSkipsProbesAndAllowsRealmOnly(t *testing.T) {
	agent := newLiteModeTestAgent(t)
	r := agent.h.repo
	if err := r.DB().Model(&model.Node{}).Where("id = ?", 1).Update("forward_mode", "lite").Error; err != nil {
		t.Fatal(err)
	}
	if err := r.DB().Create(&model.ViteConfig{
		Name:  latencyTargetsConfigKey,
		Value: `[{"name":"cf","host":"1.1.1.1","port":443,"enabled":true}]`,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := agent.h.probeNodeLatency(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.h.traceNodeRoutes(context.Background(), nil, true); err != nil {
		t.Fatal(err)
	}
	if n := len(agent.commandsOfType("TcpPing")); n != 0 {
		t.Fatalf("expected lite node to skip TcpPing, got %d commands", n)
	}
	if n := len(agent.commandsOfType("Traceroute")); n != 0 {
		t.Fatalf("expected lite node to skip Traceroute, got %d commands", n)
	}

	gostRec := httptest.NewRecorder()
	agent.h.tunnelCreate(gostRec, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/create", strings.NewReader(`{
		"name":"lite-gost","type":1,"flow":1,"trafficRatio":1,"inNodeId":[{"nodeId":1}]
	}`)))
	if !strings.Contains(gostRec.Body.String(), "轻量模式") {
		t.Fatalf("expected lite node to reject GOST tunnel, got: %s", gostRec.Body.String())
	}

	realmRec := httptest.NewRecorder()
	agent.h.tunnelCreate(realmRec, httptest.NewRequest(http.MethodPost, "/api/v1/tunnel/create", strings.NewReader(`{
		"name":"lite-realm","type":1,"engine":"realm","flow":1,"trafficRatio":1,"inNodeId":[{"nodeId":1}]
	}`)))
	var env struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(realmRec.Body.Bytes(), &env); err != nil || env.Code != 0 {
		t.Fatalf("expected lite node to allow realm tunnel, got: %s (%v)", realmRec.Body.String(), err)
	}

	modes := make([]string, 0)
	nodes, err := r.ListMonitorNodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		modes = append(modes, n.ForwardMode)
	}
	if !slices.Contains(modes, "lite") {
		t.Fatalf("expected ListMonitorNodes to select forward_mode=lite, got %v", modes)
	}
}
