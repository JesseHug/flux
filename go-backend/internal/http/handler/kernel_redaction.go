package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
)

const redactedSecretValue = "***REDACTED***"

var sensitiveJSONKeys = map[string]bool{
	"password":          true,
	"private_key":       true,
	"privateKey":        true,
	"secretKey":         true,
	"decryption":        true,
	"psk":               true,
	"api_token":         true,
	"access_key_secret": true,
	"shadowTlsPassword": true,
	"key":               true,
	"key_path":          false,
	"certificate":       true,
}

func redactSensitiveJSON(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			if sensitiveJSONKeys[k] {
				switch s := val.(type) {
				case string:
					if strings.TrimSpace(s) != "" {
						out[k] = redactedSecretValue
						continue
					}
				case []interface{}:
					if len(s) > 0 {
						out[k] = []string{redactedSecretValue}
						continue
					}
				}
			}
			out[k] = redactSensitiveJSON(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, item := range t {
			out[i] = redactSensitiveJSON(item)
		}
		return out
	default:
		return v
	}
}

func redactKernelFiles(raw interface{}) []kernel.File {
	files, _ := raw.([]kernel.File)
	if len(files) == 0 {
		return []kernel.File{}
	}
	out := make([]kernel.File, len(files))
	for i, f := range files {
		out[i].Name = f.Name
		if strings.HasSuffix(strings.ToLower(f.Name), ".json") {
			var parsed interface{}
			if err := json.Unmarshal([]byte(f.Content), &parsed); err == nil {
				redacted := redactSensitiveJSON(parsed)
				if b, err := json.MarshalIndent(redacted, "", "  "); err == nil {
					out[i].Content = string(b) + "\n"
					continue
				}
			}
		}
		out[i].Content = f.Content
	}
	return out
}

func (h *Handler) kernelRendered(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID         int64  `json:"id"`
		NodeID     int64  `json:"nodeId"`
		KernelType string `json:"kernelType"`
		Logs       bool   `json:"logs"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}

	var k *model.NodeKernel
	var err error
	switch {
	case req.ID > 0:
		k, err = h.repo.GetNodeKernel(req.ID)
	case req.NodeID > 0 && strings.TrimSpace(req.KernelType) != "":
		k, err = h.repo.GetNodeKernelByType(req.NodeID, strings.ToLower(strings.TrimSpace(req.KernelType)))
	default:
		response.WriteJSON(w, response.ErrDefault("请提供 id，或同时提供 nodeId 与 kernelType"))
		return
	}
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if k == nil {
		response.WriteJSON(w, response.ErrDefault("内核不存在"))
		return
	}

	payload, hash, err := h.buildKernelApplyPayload(k)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault(err.Error()))
		return
	}
	node, _ := h.repo.GetNodeByID(k.NodeID)
	out := map[string]interface{}{
		"id":               k.ID,
		"nodeId":           k.NodeID,
		"nodeName":         "",
		"nodeOnline":       false,
		"kernelType":       k.KernelType,
		"version":          k.Version,
		"installedVersion": k.InstalledVersion,
		"status":           kernelStatusOnNode(k.Status, node),
		"lastError":        k.LastError,
		"enabled":          k.Enabled == 1,
		"configHash":       hash,
		"appliedHash":      k.AppliedHash,
		"synced":           hash != "" && hash == k.AppliedHash,
		"args":             payload["args"],
		"files":            redactKernelFiles(payload["files"]),
		"ports":            payload["ports"],
		"redacted":         true,
	}
	if node != nil {
		out["nodeName"] = node.Name
		out["nodeOnline"] = node.Status == 1
	}

	if node != nil && node.Status == 1 {
		params := map[string]interface{}{}
		if req.Logs {
			params["logs"] = k.KernelType
		}
		res, err := h.sendNodeCommandWithTimeout(k.NodeID, "KernelStatus", params, kernelCommandTimeout, false, false)
		if err == nil {
			var status agentKernelStatus
			if err := remarshal(res.Data, &status); err == nil {
				for _, ks := range status.Kernels {
					if ks.Type == k.KernelType {
						out["agentRunning"] = ks.Running
						out["agentVersion"] = ks.Version
						out["agentConfigHash"] = ks.ConfigHash
						out["agentError"] = ks.Error
						out["agentInSync"] = hash != "" && ks.ConfigHash == hash
						if req.Logs {
							out["logs"] = ks.Logs
						}
						break
					}
				}
			}
		} else {
			out["agentStatusError"] = err.Error()
		}
	}

	response.WriteJSON(w, response.OK(out))
}
