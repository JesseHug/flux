package contract_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go-backend/internal/auth"
)

func TestSubscriptionRoundTripAndRefCleanupContract(t *testing.T) {
	secret := "contract-jwt-secret"
	router, r := setupContractRouter(t, secret)
	admin, _ := auth.GenerateToken(1, "admin_user", 0, secret)

	nodeID := seedKernelNode(t, r, "sub-node")
	now := time.Now().UnixMilli()
	if err := r.DB().Exec(`INSERT INTO node_kernel(node_id, kernel_type, version, status, created_time, updated_time) VALUES(?, 'singbox', 'latest', 'running', ?, ?)`, nodeID, now, now).Error; err != nil {
		t.Fatal(err)
	}

	makeInbound := func(name string, port int) int64 {
		res := kernelContractPost(t, router, admin, "/api/v1/inbound/create",
			`{"nodeId":`+jsonInt64(nodeID)+`,"kernelType":"singbox","name":"`+name+`","protocol":"shadowsocks","port":`+jsonInt64(int64(port))+`}`)
		if res.Code != 0 {
			t.Fatalf("create %s: %+v", name, res)
		}
		return int64(res.Data.(map[string]interface{})["id"].(float64))
	}

	inA := makeInbound("HK-Home-Old", 19001)
	inRelay := makeInbound("SZ-Relay", 19002)
	dup1 := makeInbound("dup-node", 19003)
	dup2 := makeInbound("dup-node", 19004)
	_ = dup2

	groupID := saveNodeGroup(t, router, admin, `{"name":"All","sourceAll":true}`)
	subSave := kernelContractPost(t, router, admin, "/api/v1/subscription/save",
		`{"name":"Main","groupIds":[`+jsonInt64(groupID)+`],"remark":"keep-remark","options":{"chainLandingNames":["HK-Home-Old","dup-node","stale-orphan"],"chainRelayNames":["SZ-Relay"],"chainNodeBindings":{"HK-Home-Old":"SZ-Relay","stale-orphan":"SZ-Relay"}}}`)
	if subSave.Code != 0 {
		t.Fatalf("save sub: %+v", subSave)
	}
	subID := int64(subSave.Data.(map[string]interface{})["id"].(float64))

	t.Run("subscription/list item round-trips cleanly and omits preserve existing values", func(t *testing.T) {
		listRes := kernelContractPost(t, router, admin, "/api/v1/subscription/list", `{}`)
		if listRes.Code != 0 {
			t.Fatalf("list: %+v", listRes)
		}
		items := listRes.Data.(map[string]interface{})["items"].([]interface{})
		var target map[string]interface{}
		for _, it := range items {
			m := it.(map[string]interface{})
			if int64(m["id"].(float64)) == subID {
				target = m
				break
			}
		}
		if target == nil {
			t.Fatal("created subscription missing from list")
		}
		// Ensure sharedIds (and token/path/active/nodeCount) are accepted without error.
		target["name"] = "Main-Edited"
		delete(target, "options")
		delete(target, "remark")
		roundTrip := kernelContractPost(t, router, admin, "/api/v1/subscription/save", mustJSON(t, target))
		if roundTrip.Code != 0 {
			t.Fatalf("round-trip subscription/save with sharedIds failed: %+v", roundTrip)
		}
		saved := roundTrip.Data.(map[string]interface{})
		if saved["remark"] != "keep-remark" {
			t.Fatalf("omitted remark wiped: %+v", saved)
		}
		opts := saved["options"].(map[string]interface{})
		if !strings.Contains(mustJSON(t, opts["chainLandingNames"]), "HK-Home-Old") {
			t.Fatalf("omitted options wiped: %+v", opts)
		}
	})

	t.Run("inbound rename and delete cascade cleanly without dropping duplicate names", func(t *testing.T) {
		patchRes := kernelContractPost(t, router, admin, "/api/v1/inbound/patch",
			`{"id":`+jsonInt64(inA)+`,"name":"HK-Home-New"}`)
		if patchRes.Code != 0 {
			t.Fatalf("patch rename: %+v", patchRes)
		}

		subRow, err := r.GetSubscription(subID)
		if err != nil || subRow == nil {
			t.Fatalf("get sub: %v", err)
		}
		var optsMap map[string]interface{}
		_ = json.Unmarshal([]byte(subRow.Options), &optsMap)
		landingJSON := mustJSON(t, optsMap["chainLandingNames"])
		bindingsJSON := mustJSON(t, optsMap["chainNodeBindings"])
		if !strings.Contains(landingJSON, "HK-Home-New") || strings.Contains(landingJSON, "HK-Home-Old") {
			t.Fatalf("rename did not cascade into chainLandingNames: %s", landingJSON)
		}
		if !strings.Contains(bindingsJSON, `"HK-Home-New":"SZ-Relay"`) {
			t.Fatalf("rename did not cascade into chainNodeBindings: %s", bindingsJSON)
		}

		// Delete one duplicate inbound -> dup-node still exists via dup2, must stay.
		if del := kernelContractPost(t, router, admin, "/api/v1/inbound/delete", `{"id":`+jsonInt64(dup1)+`}`); del.Code != 0 {
			t.Fatalf("delete dup1: %+v", del)
		}
		subRow, _ = r.GetSubscription(subID)
		if !strings.Contains(subRow.Options, "dup-node") {
			t.Fatalf("duplicate inbound name still in use was falsely pruned: %s", subRow.Options)
		}

		// Delete unique inbound inA -> HK-Home-New must be removed from landing names & bindings.
		if del := kernelContractPost(t, router, admin, "/api/v1/inbound/delete", `{"id":`+jsonInt64(inA)+`}`); del.Code != 0 {
			t.Fatalf("delete inA: %+v", del)
		}
		_ = inRelay
		subRow, _ = r.GetSubscription(subID)
		if strings.Contains(subRow.Options, "HK-Home-New") {
			t.Fatalf("deleted unique inbound stayed in subscription options: %s", subRow.Options)
		}
	})

	t.Run("prune-refs cleans stale orphan references", func(t *testing.T) {
		dry := kernelContractPost(t, router, admin, "/api/v1/subscription/prune-refs", `{"dryRun":true}`)
		if dry.Code != 0 || valueAsInt(dry.Data.(map[string]interface{})["updatedSubscriptions"]) != 1 {
			t.Fatalf("prune dryRun: %+v", dry)
		}
		subRow, _ := r.GetSubscription(subID)
		if !strings.Contains(subRow.Options, "stale-orphan") {
			t.Fatalf("dryRun mutated DB: %s", subRow.Options)
		}

		apply := kernelContractPost(t, router, admin, "/api/v1/subscription/prune-refs", `{}`)
		if apply.Code != 0 || valueAsInt(apply.Data.(map[string]interface{})["updatedSubscriptions"]) != 1 {
			t.Fatalf("prune apply: %+v", apply)
		}
		subRow, _ = r.GetSubscription(subID)
		if strings.Contains(subRow.Options, "stale-orphan") {
			t.Fatalf("prune-refs left orphan behind: %s", subRow.Options)
		}
	})
}
