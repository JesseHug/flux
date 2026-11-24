package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
)

func TestCFPoolAndCDNSubscriptionContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	// Allow httptest local server fetches for URL pool sync.
	if err := r.UpsertConfig("fetch_allow_local", "true", time.Now().UnixMilli()); err != nil {
		t.Fatalf("set fetch_allow_local: %v", err)
	}

	var failUpstream atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if failUpstream.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("104.16.132.229:443#移动优选\n172.67.1.2:443#电信优选\n"))
	}))
	defer upstream.Close()

	// 1. Parse preview & manual pool.
	prevRes := kernelContractPost(t, router, admin, "/api/v1/cf-pool/parse-preview",
		`{"rawText":"104.18.2.2:443#联通\ncf.877774.xyz:2053#优选域名","defaultPort":443,"maxEntries":10,"ipFilter":"all"}`)
	if prevRes.Code != 0 {
		t.Fatalf("parse-preview failed: %+v", prevRes)
	}
	prevData := prevRes.Data.(map[string]interface{})
	if int(prevData["count"].(float64)) != 2 {
		t.Fatalf("expected 2 preview entries, got %+v", prevData)
	}

	// 2. URL pool initial sync + failure keeps cached entries.
	saveURL := kernelContractPost(t, router, admin, "/api/v1/cf-pool/save", mustJSON(t, map[string]interface{}{
		"name":          "三网优选源",
		"kind":          "url",
		"url":           upstream.URL,
		"intervalHours": 6,
		"defaultPort":   443,
		"maxEntries":    10,
		"ipFilter":      "all",
		"enabled":       true,
	}))
	if saveURL.Code != 0 {
		t.Fatalf("save URL pool failed: %+v", saveURL)
	}
	poolMap := saveURL.Data.(map[string]interface{})
	poolID := int64(poolMap["id"].(float64))
	if int(poolMap["entryCount"].(float64)) != 2 {
		t.Fatalf("expected 2 entries on URL pool, got %+v", poolMap)
	}

	failUpstream.Store(true)
	refreshFail := kernelContractPost(t, router, admin, "/api/v1/cf-pool/refresh", `{"id":`+jsonInt64(poolID)+`}`)
	if refreshFail.Code == 0 {
		t.Fatalf("expected refresh failure when upstream returns 500")
	}
	listedPools := kernelContractPost(t, router, admin, "/api/v1/cf-pool/list", `{}`)
	if listedPools.Code != 0 {
		t.Fatalf("cf-pool/list failed: %+v", listedPools)
	}
	poolsArr := listedPools.Data.([]interface{})
	firstPool := poolsArr[0].(map[string]interface{})
	if int(firstPool["entryCount"].(float64)) != 2 || firstPool["lastError"] == "" {
		t.Fatalf("expected cached entries preserved and lastError populated: %+v", firstPool)
	}

	// 3. Seed node + xray kernel and create a NAT-port VLESS+WS+TLS inbound behind CF Origin Rules -> 443.
	nodeID := seedKernelNode(t, r, "dr-cf")
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, 'xray', 'latest', 'running', ?, ?)`, nodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	certPEM, keyPEM, err := kernel.SelfSignedCert("cdn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	inRes := kernelContractPost(t, router, admin, "/api/v1/inbound/create", mustJSON(t, map[string]interface{}{
		"nodeId":     nodeID,
		"kernelType": "xray",
		"name":       "DR-CF",
		"protocol":   "vless",
		"port":       57501,
		"transport": map[string]interface{}{
			"type": "ws",
			"path": "/cfws",
		},
		"security": map[string]interface{}{
			"type":     "tls",
			"sni":      "cdn.example.com",
			"certMode": "pem",
			"certPem":  certPEM,
			"keyPem":   keyPEM,
		},
		"cdn": map[string]interface{}{
			"enabled":        true,
			"domain":         "cdn.example.com",
			"edgePort":       443,
			"poolId":         poolID,
			"customEntries":  "cf.877774.xyz:443#优选域名",
			"keepOriginHost": true,
			"onlyCfOrigin":   true,
		},
	}))
	if inRes.Code != 0 {
		t.Fatalf("create CF CDN inbound failed: %+v", inRes)
	}
	inID := int64(inRes.Data.(map[string]interface{})["id"].(float64))

	// 4. Verify rendered kernel includes CF origin CIDRs in port allowlist and hides PEM in list.
	rendered := kernelContractPost(t, router, admin, "/api/v1/kernel/rendered",
		`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"xray"}`)
	if rendered.Code != 0 {
		t.Fatalf("kernel/rendered failed: %+v", rendered)
	}
	renderedData := rendered.Data.(map[string]interface{})
	portsJSON := mustJSON(t, renderedData["ports"])
	if !strings.Contains(portsJSON, "104.16.0.0/13") || !strings.Contains(portsJSON, "2606:4700::/32") {
		t.Fatalf("expected Cloudflare origin CIDRs in port allowlist, got: %s", portsJSON)
	}
	inList := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
	if inList.Code != 0 {
		t.Fatalf("inbound/list failed: %+v", inList)
	}
	if strings.Contains(mustJSON(t, inList.Data), "BEGIN EC PRIVATE KEY") {
		t.Fatal("inbound/list must not expose private key PEM")
	}

	// 5. Verify /inbound/share expands 3 optimal entries + 1 fallback origin host, with TrustedEdgeTLS.
	shareRes := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(inID)+`}`)
	if shareRes.Code != 0 {
		t.Fatalf("inbound/share failed: %+v", shareRes)
	}
	entries := shareRes.Data.([]interface{})
	if len(entries) != 4 {
		t.Fatalf("expected 4 expanded CDN share entries, got %d: %+v", len(entries), entries)
	}
	shareJSON := mustJSON(t, entries)
	if !strings.Contains(shareJSON, `"kind":"cdn"`) || !strings.Contains(shareJSON, "104.16.132.229") || !strings.Contains(shareJSON, "cf.877774.xyz") {
		t.Fatalf("missing expanded CF entries in share response: %s", shareJSON)
	}
	if strings.Contains(shareJSON, "allowInsecure") {
		t.Fatalf("CF CDN share links must verify edge TLS (no allowInsecure): %s", shareJSON)
	}

	// 6. Verify subscription export (Clash / sing-box / Base64).
	groupID := saveNodeGroup(t, router, admin, `{"name":"CF-All","sourceAll":true}`)
	subSave := kernelContractPost(t, router, admin, "/api/v1/subscription/save", mustJSON(t, map[string]interface{}{
		"name":     "CF订阅",
		"kind":     "share",
		"groupIds": []int64{groupID},
		"enabled":  true,
	}))
	if subSave.Code != 0 {
		t.Fatalf("subscription/save failed: %+v", subSave)
	}
	subToken := subSave.Data.(map[string]interface{})["token"].(string)

	for _, format := range []string{"clash", "singbox", "base64"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/open_api/sub/"+subToken+"?format="+format, nil)
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("sub %s status=%d body=%s", format, rec.Code, rec.Body.String())
		}
		body := rec.Body.String()
		if format != "base64" {
			if !strings.Contains(body, "104.16.132.229") || !strings.Contains(body, "cf.877774.xyz") || !strings.Contains(body, "cdn.example.com") {
				t.Fatalf("sub %s missing CF nodes:\n%s", format, body)
			}
			if strings.Contains(body, "skip-cert-verify") || strings.Contains(body, `"insecure":true`) {
				t.Fatalf("sub %s must not skip TLS verification:\n%s", format, body)
			}
		}
	}

	// 7. Referenced pool delete requires force=true; force=true unbinds inbound cleanly.
	delBlocked := kernelContractPost(t, router, admin, "/api/v1/cf-pool/delete", `{"id":`+jsonInt64(poolID)+`}`)
	if delBlocked.Code == 0 {
		t.Fatal("deleting referenced CF pool without force must fail")
	}
	delForce := kernelContractPost(t, router, admin, "/api/v1/cf-pool/delete", `{"id":`+jsonInt64(poolID)+`,"force":true}`)
	if delForce.Code != 0 {
		t.Fatalf("force delete CF pool failed: %+v", delForce)
	}
	updatedRow, err := r.GetNodeInbound(inID)
	if err != nil || updatedRow == nil {
		t.Fatalf("get inbound after force delete: %v", err)
	}
	var afterCDN kernel.CDNConfig
	if err := json.Unmarshal([]byte(updatedRow.CDNConfig), &afterCDN); err != nil || afterCDN.PoolID != 0 {
		t.Fatalf("expected poolId unbound after force delete, got: %s", updatedRow.CDNConfig)
	}

	// 8. Backup export / import round-trip preserves CFPools and NodeInbound.CDNConfig.
	poolBackupSave := kernelContractPost(t, router, admin, "/api/v1/cf-pool/save", mustJSON(t, map[string]interface{}{
		"name":       "导出保留池",
		"kind":       "manual",
		"rawText":    "172.64.0.1#CF\n",
		"ipFilter":   "all",
		"maxEntries": 10,
		"enabled":    true,
	}))
	if poolBackupSave.Code != 0 {
		t.Fatalf("cf-pool/save before backup failed: %+v", poolBackupSave)
	}
	exportReq := httptest.NewRequest(http.MethodPost, "/api/v1/backup/export", bytes.NewBufferString(`{"types":["inbounds"]}`))
	exportReq.Header.Set("Authorization", admin)
	exportResp := httptest.NewRecorder()
	router.ServeHTTP(exportResp, exportReq)
	if exportResp.Code != http.StatusOK {
		t.Fatalf("backup/export status=%d body=%s", exportResp.Code, exportResp.Body.String())
	}
	var exported model.BackupData
	if err := json.NewDecoder(exportResp.Body).Decode(&exported); err != nil {
		t.Fatalf("unmarshal backup: %v", err)
	}
	if exported.Inbounds == nil || len(exported.Inbounds.Inbounds) != 1 || exported.Inbounds.Inbounds[0].CDNConfig == "" {
		t.Fatalf("expected inbound CDNConfig in backup export: %+v", exported.Inbounds)
	}
	if len(exported.Inbounds.CFPools) != 1 || exported.Inbounds.CFPools[0].Name != "导出保留池" {
		t.Fatalf("expected CFPools in backup export: %+v", exported.Inbounds.CFPools)
	}
	for _, table := range []string{"cf_pool", "node_inbound", "node_kernel"} {
		if err := r.DB().Exec(`DELETE FROM ` + table).Error; err != nil {
			t.Fatalf("clear %s: %v", table, err)
		}
	}
	importRes := kernelContractPost(t, router, admin, "/api/v1/backup/import", mustJSON(t, map[string]interface{}{
		"version":  exported.Version,
		"types":    []string{"inbounds"},
		"inbounds": exported.Inbounds,
	}))
	if importRes.Code != 0 {
		t.Fatalf("backup/import failed: %+v", importRes)
	}
	listAfterImport := kernelContractPost(t, router, admin, "/api/v1/cf-pool/list", `{}`)
	if listAfterImport.Code != 0 {
		t.Fatalf("cf-pool/list after import failed: %+v", listAfterImport)
	}
	var poolsAfterImport []map[string]interface{}
	if err := json.Unmarshal([]byte(mustJSON(t, listAfterImport.Data)), &poolsAfterImport); err != nil {
		t.Fatalf("unmarshal cf-pool/list after import: %v", err)
	}
	if len(poolsAfterImport) != 1 || poolsAfterImport[0]["name"] != "导出保留池" {
		t.Fatalf("expected restored cf-pool after import, got %+v", poolsAfterImport)
	}
}
