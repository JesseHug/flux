package middleware

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-backend/internal/auth"
	"go-backend/internal/http/response"
)

func TestAPITokenExtractionAndScopes(t *testing.T) {
	secret := "unit-test-secret"
	jwtToken, err := auth.GenerateToken(1, "admin_user", 0, secret)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	claims, err := auth.ParseClaims(jwtToken, secret)
	if err != nil {
		t.Fatalf("parse claims: %v", err)
	}

	wrapped := JWT(AuthOptions{
		JWTSecret: secret,
		GetUserAuthState: func(userID int64) (*auth.UserAuthState, error) {
			return &auth.UserAuthState{ID: userID, RoleID: 0, Status: 1, PasswordChangedAt: claims.IatMs - 1}, nil
		},
		ValidateAPIToken: func(rawToken string, clientIP net.IP) (*APITokenIdentity, error) {
			switch rawToken {
			case "flx_readonly_secret":
				return &APITokenIdentity{
					Claims: auth.Claims{Sub: "1", User: "admin_user", RoleID: 0},
					Scope:  APITokenScopeReadonly,
				}, nil
			case "flx_operator_secret":
				return &APITokenIdentity{
					Claims: auth.Claims{Sub: "1", User: "admin_user", RoleID: 0},
					Scope:  APITokenScopeOperator,
				}, nil
			case "flx_ip_blocked":
				return nil, ErrAPITokenIPForbidden
			default:
				return nil, ErrAPITokenInvalid
			}
		},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response.WriteJSON(w, response.OK(map[string]interface{}{
			"isApiToken": IsAPITokenRequest(r),
			"scope":      APITokenScope(r),
		}))
	}))

	send := func(path string, hdrKey, hdrVal string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set(hdrKey, hdrVal)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		return rec
	}

	t.Run("header formats and JWT coexistence", func(t *testing.T) {
		assertCode(t, send("/api/v1/node/list", "X-API-Key", "flx_readonly_secret"), 0)
		assertCode(t, send("/api/v1/node/list", "Authorization", "flx_readonly_secret"), 0)
		assertCode(t, send("/api/v1/node/list", "Authorization", "Bearer flx_readonly_secret"), 0)
		assertCode(t, send("/api/v1/config/update", "Authorization", jwtToken), 0)
		assertCode(t, send("/api/v1/node/list", "X-API-Key", "flx_bad"), 401)
		assertCode(t, send("/api/v1/node/list", "X-API-Key", "flx_ip_blocked"), 403)
	})

	t.Run("readonly route matrix", func(t *testing.T) {
		for _, allowed := range []string{
			"/api/v1/node/list",
			"/api/v1/kernel/list",
			"/api/v1/kernel/rendered",
			"/api/v1/inbound/list",
			"/api/v1/inbound/share",
			"/api/v1/cf-pool/list",
			"/api/v1/cf-pool/parse-preview",
			"/api/v1/subscription/list",
			"/api/v1/subscription/preview",
			"/api/v1/tunnel/list",
			"/api/v1/forward/list",
		} {
			assertCode(t, send(allowed, "X-API-Key", "flx_readonly_secret"), 0)
		}
		for _, denied := range []string{
			"/api/v1/inbound/patch",
			"/api/v1/inbound/create",
			"/api/v1/cf-pool/save",
			"/api/v1/cf-pool/refresh",
			"/api/v1/cf-pool/delete",
			"/api/v1/subscription/save",
			"/api/v1/subscription/prune-refs",
			"/api/v1/kernel/control",
			"/api/v1/api-token/list",
			"/api/v1/user/list",
			"/api/v1/config/update",
		} {
			assertCode(t, send(denied, "X-API-Key", "flx_readonly_secret"), 403)
		}
	})

	t.Run("operator allowed and hard-blocked routes", func(t *testing.T) {
		for _, allowed := range []string{
			"/api/v1/inbound/list",
			"/api/v1/inbound/create",
			"/api/v1/inbound/update",
			"/api/v1/inbound/patch",
			"/api/v1/inbound/delete",
			"/api/v1/cf-pool/list",
			"/api/v1/cf-pool/save",
			"/api/v1/cf-pool/refresh",
			"/api/v1/cf-pool/delete",
			"/api/v1/subscription/save",
			"/api/v1/subscription/prune-refs",
			"/api/v1/kernel/rendered",
			"/api/v1/kernel/control",
			"/api/v1/forward/create",
		} {
			assertCode(t, send(allowed, "Authorization", "Bearer flx_operator_secret"), 0)
		}
		for _, hardBlocked := range []string{
			"/api/v1/api-token/list",
			"/api/v1/api-token/create",
			"/api/v1/api-token/revoke",
			"/api/v1/api-token/delete",
			"/api/v1/user/list",
			"/api/v1/user/update",
			"/api/v1/config/list",
			"/api/v1/config/update",
			"/api/v1/backup/export",
			"/api/v1/api/v1/backup/export",
			"/api/v1/system/upgrade",
			"/api/v1/system/check-updates",
			"/api/v1/federation/share/list",
			"/api/v1/announcement/update",
			"/api/v1/notify/test",
			"/api/v1/node/create",
			"/api/v1/node/update",
			"/api/v1/node/delete",
			"/api/v1/node/batch-delete",
			"/api/v1/node/install",
			"/api/v1/node/upgrade",
			"/api/v1/node/rollback",
			"/api/v1/node/tune",
			"/api/v1/node/nftables/test",
			"/api/v1/kernel/install",
			"/api/v1/tunnel/create",
		} {
			assertCode(t, send(hardBlocked, "Authorization", "Bearer flx_operator_secret"), 403)
		}
	})
}
