package contract_test

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
)

func TestInboundPartialPreserveAndDualStackContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)

	nodeID := seedKernelNode(t, r, "dual-node")
	if err := r.DB().Exec(`UPDATE node SET server_ip = '203.0.113.10', server_ip_v4 = '203.0.113.10', server_ip_v6 = '2001:db8::10' WHERE id = ?`, nodeID).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, 'singbox', 'latest', 'running', ?, ?)`, nodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}

	outRes := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
		`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"singbox","name":"exit-ss","protocol":"shadowsocks","port":18001,"relayOnly":true}`)
	if outRes.Code != 0 {
		t.Fatalf("create exit: %+v", outRes)
	}
	exitID := int64(outRes.Data.(map[string]interface{})["id"].(float64))

	mainRes := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
		`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"singbox","name":"main-in","protocol":"vless","port":18443,"security":{"type":"reality"},"outboundId":`+jsonInt64(exitID)+`,"remark":"keep-remark","shareAddressMode":"dual"}`)
	if mainRes.Code != 0 {
		t.Fatalf("create main: %+v", mainRes)
	}
	mainID := int64(mainRes.Data.(map[string]interface{})["id"].(float64))

	findMain := func() map[string]interface{} {
		listRes := kernelContractPost(t, router, admin, "/api/v1/inbound/list", `{}`)
		if listRes.Code != 0 {
			t.Fatalf("inbound/list: %+v", listRes)
		}
		for _, item := range listRes.Data.([]interface{}) {
			m := item.(map[string]interface{})
			if int64(m["id"].(float64)) == mainID {
				return m
			}
		}
		t.Fatalf("main inbound %d missing from list", mainID)
		return nil
	}

	t.Run("update omits preserve existing fields and list item round-trips cleanly", func(t *testing.T) {
		upd := kernelContractPost(t, router, admin, "/api/v1/inbound/update",
			`{"id":`+jsonInt64(mainID)+`,"name":"main-renamed"}`)
		if upd.Code != 0 {
			t.Fatalf("partial inbound/update: %+v", upd)
		}
		cur := findMain()
		if int64(cur["outboundId"].(float64)) != exitID ||
			int(cur["port"].(float64)) != 18443 ||
			cur["remark"] != "keep-remark" ||
			cur["shareAddressMode"] != "dual" {
			t.Fatalf("update cleared omitted fields: %+v", cur)
		}

		// Feed entire list item back into update.
		cur["name"] = "main-roundtrip"
		roundTrip := kernelContractPost(t, router, admin, "/api/v1/inbound/update", mustJSON(t, cur))
		if roundTrip.Code != 0 {
			t.Fatalf("list round-trip inbound/update failed: %+v", roundTrip)
		}
		cur2 := findMain()
		if cur2["name"] != "main-roundtrip" || int64(cur2["outboundId"].(float64)) != exitID {
			t.Fatalf("after round-trip update: %+v", cur2)
		}
	})

	t.Run("inbound/patch dryRun and apply", func(t *testing.T) {
		dry := kernelContractPost(t, router, admin, "/api/v1/inbound/patch",
			`{"id":`+jsonInt64(mainID)+`,"name":"main-dryrun","dryRun":true}`)
		if dry.Code != 0 {
			t.Fatalf("patch dryRun: %+v", dry)
		}
		d := dry.Data.(map[string]interface{})
		if d["dryRun"] != true || d["redacted"] != true || d["afterHash"] == "" {
			t.Fatalf("unexpected dryRun payload: %+v", d)
		}
		if findMain()["name"] != "main-roundtrip" {
			t.Fatal("dryRun must not persist changes")
		}

		patch := kernelContractPost(t, router, admin, "/api/v1/inbound/patch",
			`{"id":`+jsonInt64(mainID)+`,"name":"main-patched"}`)
		if patch.Code != 0 {
			t.Fatalf("patch apply: %+v", patch)
		}
		cur := findMain()
		if cur["name"] != "main-patched" || int64(cur["outboundId"].(float64)) != exitID {
			t.Fatalf("patch did not preserve/update properly: %+v", cur)
		}
	})

	t.Run("dual shareAddressMode exports v4 and v6 entries in share and subscription", func(t *testing.T) {
		share := kernelContractPost(t, router, admin, "/api/v1/inbound/share", `{"id":`+jsonInt64(mainID)+`}`)
		if share.Code != 0 {
			t.Fatalf("inbound/share: %+v", share)
		}
		entries := share.Data.([]interface{})
		if len(entries) != 2 {
			t.Fatalf("expected 2 dual-stack share entries, got %d: %+v", len(entries), entries)
		}
		shareJSON := mustJSON(t, entries)
		if !strings.Contains(shareJSON, "203.0.113.10") || !strings.Contains(shareJSON, "2001:db8::10") || !strings.Contains(shareJSON, "· v6") {
			t.Fatalf("dual-stack share missing v4/v6 entries: %s", shareJSON)
		}

		grpID := saveNodeGroup(t, router, admin, `{"name":"All","sourceAll":true}`)
		sub := kernelContractPost(t, router, admin, "/api/v1/subscription/save",
			`{"name":"Dual","groupIds":[`+jsonInt64(grpID)+`]}`)
		if sub.Code != 0 {
			t.Fatalf("save sub: %+v", sub)
		}
		subPath := sub.Data.(map[string]interface{})["path"].(string)
		resp := getSub(t, router, subPath+"?format=base64", "")
		raw, err := base64.StdEncoding.DecodeString(resp.Body.String())
		if err != nil {
			t.Fatalf("decode sub: %v", err)
		}
		subText := string(raw)
		if !strings.Contains(subText, "@203.0.113.10:18443") || !strings.Contains(subText, "@[2001:db8::10]:18443") {
			t.Fatalf("subscription did not export both IPv4 and IPv6 endpoints:\n%s", subText)
		}
	})
}
