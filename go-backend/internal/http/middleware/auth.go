package middleware

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
	"go-backend/internal/security"
)

type contextKey string

const (
	ClaimsContextKey        contextKey = "claims"
	APITokenScopeContextKey contextKey = "api_token_scope"

	APITokenScopeReadonly = "readonly"
	APITokenScopeOperator = "operator"

	apiTokenPrefix = "flx_"
)

var (
	ErrAPITokenInvalid     = errors.New("无效的 API 令牌或已过期")
	ErrAPITokenIPForbidden = errors.New("当前 IP 不在令牌白名单内")
)

type APITokenIdentity struct {
	Claims auth.Claims
	Scope  string
}

type AuthOptions struct {
	JWTSecret        string
	GetUserAuthState func(userID int64) (*auth.UserAuthState, error)
	ValidateAPIToken func(rawToken string, clientIP net.IP) (*APITokenIdentity, error)
}

func IsAPITokenRequest(r *http.Request) bool {
	return APITokenScope(r) != ""
}

func APITokenScope(r *http.Request) string {
	if r == nil {
		return ""
	}
	v, _ := r.Context().Value(APITokenScopeContextKey).(string)
	return v
}

func extractAPIToken(r *http.Request) (string, bool) {
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		return key, true
	}
	authHdr := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHdr == "" {
		return "", false
	}
	if strings.HasPrefix(authHdr, apiTokenPrefix) {
		return authHdr, true
	}
	if len(authHdr) > 7 && strings.EqualFold(authHdr[:7], "Bearer ") {
		rest := strings.TrimSpace(authHdr[7:])
		if strings.HasPrefix(rest, apiTokenPrefix) {
			return rest, true
		}
	}
	return "", false
}

func JWT(opts AuthOptions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkip(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			if rawAPIToken, isAPIToken := extractAPIToken(r); isAPIToken {
				if opts.ValidateAPIToken == nil {
					response.WriteJSON(w, response.Err(401, ErrAPITokenInvalid.Error()))
					return
				}
				identity, err := opts.ValidateAPIToken(rawAPIToken, security.ResolveClientIP(r))
				if err != nil || identity == nil {
					if errors.Is(err, ErrAPITokenIPForbidden) {
						response.WriteJSON(w, response.Err(403, ErrAPITokenIPForbidden.Error()))
						return
					}
					response.WriteJSON(w, response.Err(401, ErrAPITokenInvalid.Error()))
					return
				}
				if msg := checkAPITokenRoutePolicy(r.URL.Path, identity.Scope); msg != "" {
					response.WriteJSON(w, response.Err(403, msg))
					return
				}
				ctx := context.WithValue(r.Context(), ClaimsContextKey, identity.Claims)
				ctx = context.WithValue(ctx, APITokenScopeContextKey, identity.Scope)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			token := strings.TrimSpace(r.Header.Get("Authorization"))
			if token == "" {
				if allowsOptionalAuth(r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
				response.WriteJSON(w, response.Err(401, "未登录或token已过期"))
				return
			}

			claims, ok := auth.ValidateToken(token, opts.JWTSecret)
			if !ok {
				if allowsOptionalAuth(r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
				response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
				return
			}

			if opts.GetUserAuthState != nil {
				userID, err := strconv.ParseInt(claims.Sub, 10, 64)
				if err != nil {
					if allowsOptionalAuth(r.URL.Path) {
						next.ServeHTTP(w, r)
						return
					}
					response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
					return
				}
				state, err := opts.GetUserAuthState(userID)
				if err != nil || state == nil || state.Status != 1 || state.RoleID != claims.RoleID || claims.IatMs <= state.PasswordChangedAt {
					if allowsOptionalAuth(r.URL.Path) {
						next.ServeHTTP(w, r)
						return
					}
					response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
					return
				}
			}

			if requiresAdmin(r.URL.Path) && claims.RoleID != 0 {
				response.WriteJSON(w, response.Err(403, "权限不足，仅管理员可操作"))
				return
			}

			ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

var apiTokenReadonlyRoutes = map[string]bool{
	"/api/v1/node/list":                       true,
	"/api/v1/node/check-status":               true,
	"/api/v1/node/releases":                   true,
	"/api/v1/node/latency/history":            true,
	"/api/v1/node/route":                      true,
	"/api/v1/node/traffic/month":              true,
	"/api/v1/kernel/capabilities":             true,
	"/api/v1/kernel/list":                     true,
	"/api/v1/kernel/versions":                 true,
	"/api/v1/kernel/logs":                     true,
	"/api/v1/kernel/rendered":                 true,
	"/api/v1/inbound/list":                    true,
	"/api/v1/inbound/share":                   true,
	"/api/v1/inbound/sub/info":                true,
	"/api/v1/inbound/relay/list":              true,
	"/api/v1/subscription/list":               true,
	"/api/v1/subscription/traffic":            true,
	"/api/v1/subscription/preview":            true,
	"/api/v1/subscription/source/list":        true,
	"/api/v1/subscription/group/list":         true,
	"/api/v1/subscription/group/preview":      true,
	"/api/v1/subscription/template/catalog":   true,
	"/api/v1/subscription/template/list":      true,
	"/api/v1/subscription/template/check":     true,
	"/api/v1/subscription/icon-library/list":  true,
	"/api/v1/subscription/icon-library/icons": true,
	"/api/v1/tunnel/list":                     true,
	"/api/v1/tunnel/get":                      true,
	"/api/v1/forward/list":                    true,
	"/api/v1/speed-limit/list":                true,
	"/api/v1/group/tunnel/list":               true,
	"/api/v1/group/user/list":                 true,
	"/api/v1/group/permission/list":           true,
	"/api/v1/system/version":                  true,
	"/api/v1/system/storage":                  true,
	"/api/v1/monitor/nodes":                   true,
	"/api/v1/monitor/tunnels":                 true,
	"/api/v1/monitor/tunnels/quality":         true,
	"/api/v1/monitor/services":                true,
	"/api/v1/monitor/services/latest-results": true,
}

var apiTokenOperatorExtraRoutes = map[string]bool{
	"/api/v1/kernel/control":               true,
	"/api/v1/node/latency/probe":           true,
	"/api/v1/node/latency/targets":         true,
	"/api/v1/node/route/probe":             true,
	"/api/v1/node/update-order":            true,
	"/api/v1/node/dismiss-expiry-reminder": true,
	"/api/v1/tunnel/diagnose":              true,
	"/api/v1/tunnel/diagnose/stream":       true,
	"/api/v1/tunnel/update-order":          true,
}

func isHardBlockedAPITokenPath(path string) bool {
	switch {
	case strings.HasPrefix(path, "/api/v1/api-token/"):
		return true
	case strings.HasPrefix(path, "/api/v1/user/"):
		return true
	case strings.HasPrefix(path, "/api/v1/backup/") || strings.HasPrefix(path, "/api/v1/api/v1/backup/"):
		return true
	case strings.HasPrefix(path, "/api/v1/config/"):
		return true
	case strings.HasPrefix(path, "/api/v1/system/upgrade") || strings.HasPrefix(path, "/api/v1/system/check-updates"):
		return true
	case strings.HasPrefix(path, "/api/v1/federation/"):
		return true
	case strings.HasPrefix(path, "/api/v1/announcement/"):
		return true
	case strings.HasPrefix(path, "/api/v1/notify/"):
		return true
	case path == "/api/v1/kernel/install":
		return true
	}
	switch path {
	case "/api/v1/node/create", "/api/v1/node/update", "/api/v1/node/delete", "/api/v1/node/batch-delete",
		"/api/v1/node/install", "/api/v1/node/upgrade", "/api/v1/node/batch-upgrade", "/api/v1/node/rollback", "/api/v1/node/tune":
		return true
	}
	if strings.HasPrefix(path, "/api/v1/node/nftables/") {
		return true
	}
	return false
}

func checkAPITokenRoutePolicy(path, scope string) string {
	if isHardBlockedAPITokenPath(path) {
		return "该高危操作禁止使用 API 令牌，请登录网页后台执行"
	}
	if apiTokenReadonlyRoutes[path] {
		return ""
	}
	if scope == APITokenScopeReadonly {
		return "只读令牌无权执行此操作"
	}
	if scope != APITokenScopeOperator {
		return "令牌权限不足"
	}
	if strings.HasPrefix(path, "/api/v1/inbound/") ||
		strings.HasPrefix(path, "/api/v1/subscription/") ||
		strings.HasPrefix(path, "/api/v1/forward/") ||
		apiTokenOperatorExtraRoutes[path] {
		return ""
	}
	return "该操作不在 operator 令牌白名单内，请登录网页后台执行"
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Context().Value(ClaimsContextKey)
		claims, ok := raw.(auth.Claims)
		if !ok {
			response.WriteJSON(w, response.Err(401, "无法获取用户权限信息"))
			return
		}
		if claims.RoleID != 0 {
			response.WriteJSON(w, response.Err(403, "权限不足，仅管理员可操作"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowsOptionalAuth(path string) bool {
	return path == "/api/v1/config/get"
}

func shouldSkip(path string) bool {
	switch {
	case strings.HasPrefix(path, "/flow/"):
		return true
	case strings.HasPrefix(path, "/api/v1/open_api/"):
		return true
	case strings.HasPrefix(path, "/api/v1/captcha/"):
		return true
	case path == "/api/v1/config/get":
		return false
	case path == "/api/v1/user/login":
		return true
	case path == "/api/v1/user/passkey/status" || path == "/api/v1/user/passkey/login/begin" || path == "/api/v1/user/passkey/login/finish":
		return true
	case path == "/api/v1/user/github/status" || path == "/api/v1/user/github/login/begin" || path == "/api/v1/user/github/login/finish":
		return true
	case path == "/api/v1/public/config/get":
		return true
	case path == "/api/v1/federation/connect":
		return true
	case path == "/api/v1/federation/tunnel/create":
		return true
	case path == "/api/v1/federation/runtime/reserve-port":
		return true
	case path == "/api/v1/federation/runtime/apply-role":
		return true
	case path == "/api/v1/federation/runtime/release-role":
		return true
	case path == "/api/v1/federation/runtime/diagnose":
		return true
	case path == "/api/v1/federation/runtime/command":
		return true
	default:
		return false
	}
}

func requiresAdmin(path string) bool {
	if strings.HasPrefix(path, "/api/v1/monitor/permission/") || strings.HasPrefix(path, "/api/v1/api-token/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/system/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/group/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/federation/share/") || strings.HasPrefix(path, "/api/v1/federation/node/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/node/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/kernel/") || strings.HasPrefix(path, "/api/v1/inbound/") ||
		strings.HasPrefix(path, "/api/v1/notify/") || strings.HasPrefix(path, "/api/v1/subscription/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/speed-limit/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/backup/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/api/v1/backup/") {
		return true
	}

	if strings.HasPrefix(path, "/api/v1/tunnel/") {
		if strings.HasPrefix(path, "/api/v1/tunnel/user/tunnel") {
			return false
		}
		return true
	}

	switch path {
	case "/api/v1/user/create", "/api/v1/user/list", "/api/v1/user/update", "/api/v1/user/delete", "/api/v1/user/reset", "/api/v1/user/quota/reset":
		return true
	case "/api/v1/config/update", "/api/v1/config/update-single":
		return true
	case "/api/v1/announcement/update":
		return true
	default:
		return false
	}
}
