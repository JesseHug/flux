package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/security"
	"go-backend/internal/store/repo"
)

func (h *Handler) authPeer(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			response.WriteJSON(w, response.Err(401, "Missing Authorization header"))
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.WriteJSON(w, response.Err(401, "Invalid Authorization format"))
			return
		}

		token := parts[1]
		share, err := h.repo.GetPeerShareByToken(token)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		if share == nil {
			response.WriteJSON(w, response.Err(401, "Invalid token"))
			return
		}

		if strings.TrimSpace(share.AllowedIPs) != "" {
			clientIP := resolvePeerClientIP(r)
			if clientIP == nil {
				response.WriteJSON(w, response.Err(403, "Unable to determine client IP"))
				return
			}
			if !isPeerIPAllowed(clientIP, share.AllowedIPs) {
				response.WriteJSON(w, response.Err(403, "IP not allowed"))
				return
			}
		}

		if share.AllowedDomains != "" {
			clientDomain := r.Header.Get("X-Panel-Domain")
			if clientDomain == "" {
				response.WriteJSON(w, response.Err(403, "Domain verification required"))
				return
			}
			allowed := false
			domains := strings.Split(share.AllowedDomains, ",")
			for _, d := range domains {
				if strings.TrimSpace(d) == clientDomain {
					allowed = true
					break
				}
			}
			if !allowed {
				response.WriteJSON(w, response.Err(403, "Domain not allowed"))
				return
			}
		}

		if share.IsActive != 1 || (share.ExpiryTime > 0 && share.ExpiryTime <= time.Now().UnixMilli()) || isPeerShareFlowExceeded(share) {
			if !isFederationCleanupRequest(r) {
				response.WriteJSON(w, response.Err(403, "Share is inactive, expired, or over quota"))
				return
			}
		}

		next(w, r)
	}
}

// Revoked allocation privileges must not revoke cleanup privileges. Inspect
// only supported destructive commands, preserving the body for the handler.
func isFederationCleanupRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/federation/runtime/release-role":
		return true
	case "/api/v1/federation/runtime/command":
		if r.Body == nil {
			return false
		}
		var copied bytes.Buffer
		original := r.Body
		var request federationRuntimeCommandRequest
		err := json.NewDecoder(io.TeeReader(io.LimitReader(original, 1<<20), &copied)).Decode(&request)
		r.Body = struct {
			io.Reader
			io.Closer
		}{Reader: io.MultiReader(&copied, original), Closer: original}
		if err != nil || !isFederationRuntimeCommandAllowed(request.CommandType) {
			return false
		}
		_, action := federationResourceCommandKind(request.CommandType)
		return action == "delete"
	default:
		return false
	}
}

func (h *Handler) federationConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("Invalid method"))
		return
	}

	token := extractBearerToken(r)
	share, err := h.repo.GetPeerShareByToken(token)
	if err != nil || share == nil {
		response.WriteJSON(w, response.Err(401, "Unauthorized"))
		return
	}

	nodeInfo, err := h.repo.GetNodeBasicInfo(share.NodeID)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, "Node not found"))
		return
	}

	response.WriteJSON(w, response.OK(map[string]interface{}{
		"shareId":        share.ID,
		"shareName":      share.Name,
		"nodeId":         share.NodeID,
		"nodeName":       nodeInfo.Name,
		"serverIp":       nodeInfo.ServerIP,
		"status":         nodeInfo.Status,
		"maxBandwidth":   share.MaxBandwidth,
		"currentFlow":    share.CurrentFlow,
		"expiryTime":     share.ExpiryTime,
		"portRangeStart": share.PortRangeStart,
		"portRangeEnd":   share.PortRangeEnd,
	}))
}

func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	parts := strings.Split(authHeader, " ")
	if len(parts) == 2 && parts[0] == "Bearer" {
		return parts[1]
	}
	return ""
}

func isPeerShareFlowExceeded(share *repo.PeerShare) bool {
	if share == nil {
		return false
	}
	if share.MaxBandwidth <= 0 {
		return false
	}
	return share.CurrentFlow >= share.MaxBandwidth
}

func normalizePeerShareAllowedIPs(raw string) (string, error) {
	return security.NormalizeAllowedIPs(raw)
}

func resolvePeerClientIP(r *http.Request) net.IP {
	return security.ResolveClientIP(r)
}

func isPeerIPAllowed(clientIP net.IP, whitelist string) bool {
	return security.IsIPAllowed(clientIP, whitelist)
}
