package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
)

func apiTokenPost(t *testing.T, router http.Handler, hdrKey, hdrVal, path, body string, remoteAddr string) response.R {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if hdrKey != "" {
		req.Header.Set(hdrKey, hdrVal)
	}
	req.Header.Set("Content-Type", "application/json")
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	var out response.R
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v (raw=%s)", path, err, res.Body.String())
	}
	return out
}

func TestAPITokenLifecycleAndSecurityContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	nodeID := seedKernelNode(t, r, "api-node")
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, 'singbox', 'latest', 'running', ?, ?)`, nodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}
	kernelID := mustLastInsertID(t, r, "node_kernel")
	inRes := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
		`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"singbox","name":"ss-node","protocol":"shadowsocks","port":18388,"settings":{"method":"aes-256-gcm","password":"super-secret-ss-password"}}`)
	if inRes.Code != 0 {
		t.Fatalf("create inbound: %+v", inRes)
	}
	inID := int64(inRes.Data.(map[string]interface{})["id"].(float64))

	t.Run("admin scope rejected and plaintext shown once", func(t *testing.T) {
		bad := kernelContractPost(t, router, admin, "/api/v1/api-token/create",
			`{"name":"full-admin","scope":"admin"}`)
		if bad.Code == 0 {
			t.Fatalf("admin scope must be rejected: %+v", bad)
		}

		res := kernelContractPost(t, router, admin, "/api/v1/api-token/create",
			`{"name":"ci-ro","scope":"readonly","expiresDays":30}`)
		if res.Code != 0 {
			t.Fatalf("create readonly: %+v", res)
		}
		data := res.Data.(map[string]interface{})
		raw, _ := data["token"].(string)
		if !strings.HasPrefix(raw, "flx_") || len(raw) < 20 {
			t.Fatalf("unexpected raw token: %q", raw)
		}
		listed := kernelContractPost(t, router, admin, "/api/v1/api-token/list", `{}`)
		if listed.Code != 0 {
			t.Fatalf("list tokens: %+v", listed)
		}
		listJSON := mustJSON(t, listed.Data)
		if strings.Contains(listJSON, raw) {
			t.Fatal("api-token/list must never expose plaintext or full token")
		}
	})

	roCreate := kernelContractPost(t, router, admin, "/api/v1/api-token/create", `{"name":"ro","scope":"readonly"}`)
	roToken := roCreate.Data.(map[string]interface{})["token"].(string)

	opCreate := kernelContractPost(t, router, admin, "/api/v1/api-token/create", `{"name":"op","scope":"operator"}`)
	opData := opCreate.Data.(map[string]interface{})
	opToken := opData["token"].(string)
	opID := int64(opData["item"].(map[string]interface{})["id"].(float64))

	t.Run("readonly inspection and redaction", func(t *testing.T) {
		list := apiTokenPost(t, router, "X-API-Key", roToken, "/api/v1/inbound/list", `{}`, "")
		if list.Code != 0 {
			t.Fatalf("readonly inbound/list: %+v", list)
		}

		rendered := apiTokenPost(t, router, "Authorization", "Bearer "+roToken, "/api/v1/kernel/rendered",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"singbox"}`, "")
		if rendered.Code != 0 {
			t.Fatalf("readonly kernel/rendered: %+v", rendered)
		}
		rdata := rendered.Data.(map[string]interface{})
		if rdata["configHash"] == "" || rdata["redacted"] != true {
			t.Fatalf("expected hash & redacted flag: %+v", rdata)
		}
		filesJSON := mustJSON(t, rdata["files"])
		if strings.Contains(filesJSON, "super-secret-ss-password") || !strings.Contains(filesJSON, "***REDACTED***") {
			t.Fatalf("rendered config leaked secret or missed redaction: %s", filesJSON)
		}

		denied := apiTokenPost(t, router, "X-API-Key", roToken, "/api/v1/inbound/patch",
			`{"id":`+jsonInt64(inID)+`,"name":"blocked"}`, "")
		if denied.Code != 403 {
			t.Fatalf("readonly patch expected 403, got %+v", denied)
		}
	})

	t.Run("operator allowed ops and hard blocks", func(t *testing.T) {
		patch := apiTokenPost(t, router, "Authorization", opToken, "/api/v1/inbound/patch",
			`{"id":`+jsonInt64(inID)+`,"name":"ss-node-v2"}`, "")
		if patch.Code != 0 {
			t.Fatalf("operator inbound/patch: %+v", patch)
		}

		prune := apiTokenPost(t, router, "X-API-Key", opToken, "/api/v1/subscription/prune-refs", `{}`, "")
		if prune.Code != 0 {
			t.Fatalf("operator prune-refs: %+v", prune)
		}

		// Sync proceeds to handler (node is offline so handler returns business error, not 403).
		syncRes := apiTokenPost(t, router, "X-API-Key", opToken, "/api/v1/kernel/control",
			`{"id":`+jsonInt64(kernelID)+`,"action":"sync"}`, "")
		if syncRes.Code == 403 {
			t.Fatalf("operator kernel/control sync must not be 403: %+v", syncRes)
		}

		// Removing a kernel is blocked even for operator tokens.
		rmRes := apiTokenPost(t, router, "X-API-Key", opToken, "/api/v1/kernel/control",
			`{"id":`+jsonInt64(kernelID)+`,"action":"remove"}`, "")
		if rmRes.Code != 403 {
			t.Fatalf("operator kernel/control remove expected 403, got %+v", rmRes)
		}

		for _, blocked := range []string{
			"/api/v1/api-token/list",
			"/api/v1/config/list",
			"/api/v1/user/list",
			"/api/v1/node/delete",
		} {
			res := apiTokenPost(t, router, "X-API-Key", opToken, blocked, `{}`, "")
			if res.Code != 403 {
				t.Fatalf("operator %s expected 403, got %+v", blocked, res)
			}
		}
	})

	t.Run("ip allowlist, revoke, and expiry", func(t *testing.T) {
		cidrRes := kernelContractPost(t, router, admin, "/api/v1/api-token/create",
			`{"name":"cidr","scope":"readonly","allowedIps":"10.10.0.0/16"}`)
		if cidrRes.Code != 0 {
			t.Fatalf("create cidr token: %+v", cidrRes)
		}
		cidrToken := cidrRes.Data.(map[string]interface{})["token"].(string)

		if denied := apiTokenPost(t, router, "X-API-Key", cidrToken, "/api/v1/node/list", `{}`, "192.0.2.50:1234"); denied.Code != 403 {
			t.Fatalf("disallowed IP expected 403, got %+v", denied)
		}
		if okRes := apiTokenPost(t, router, "X-API-Key", cidrToken, "/api/v1/node/list", `{}`, "10.10.1.5:1234"); okRes.Code != 0 {
			t.Fatalf("allowed IP expected 0, got %+v", okRes)
		}

		// Revoke operator token.
		if rev := kernelContractPost(t, router, admin, "/api/v1/api-token/revoke", `{"id":`+jsonInt64(opID)+`}`); rev.Code != 0 {
			t.Fatalf("revoke: %+v", rev)
		}
		if denied := apiTokenPost(t, router, "X-API-Key", opToken, "/api/v1/node/list", `{}`, ""); denied.Code != 401 {
			t.Fatalf("revoked token expected 401, got %+v", denied)
		}

		// Force-expire cidr token.
		if err := r.DB().Exec(`UPDATE api_token SET expires_at = 1 WHERE name = 'cidr'`).Error; err != nil {
			t.Fatal(err)
		}
		if expired := apiTokenPost(t, router, "X-API-Key", cidrToken, "/api/v1/node/list", `{}`, "10.10.1.5:1234"); expired.Code != 401 {
			t.Fatalf("expired token expected 401, got %+v", expired)
		}
	})
}
