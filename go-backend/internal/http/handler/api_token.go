package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"time"

	"go-backend/internal/auth"
	"go-backend/internal/http/middleware"
	"go-backend/internal/http/response"
	"go-backend/internal/security"
	"go-backend/internal/store/model"
)

const (
	apiTokenScopeReadonly = middleware.APITokenScopeReadonly
	apiTokenScopeOperator = middleware.APITokenScopeOperator
	apiTokenPrefix        = "flx_"
	apiTokenTouchInterval = int64(60 * 1000)
	base62Alphabet        = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

type apiTokenView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Prefix      string `json:"prefix"`
	Scope       string `json:"scope"`
	AllowedIPs  string `json:"allowedIps"`
	UserID      int64  `json:"userId"`
	UserName    string `json:"userName,omitempty"`
	ExpiresAt   int64  `json:"expiresAt"`
	LastUsedAt  int64  `json:"lastUsedAt"`
	LastUsedIP  string `json:"lastUsedIp"`
	Revoked     bool   `json:"revoked"`
	Expired     bool   `json:"expired"`
	CreatedTime int64  `json:"createdTime"`
}

func apiTokenToView(row model.APIToken, userNames map[int64]string, now int64) apiTokenView {
	return apiTokenView{
		ID:          row.ID,
		Name:        row.Name,
		Prefix:      row.Prefix,
		Scope:       row.Scope,
		AllowedIPs:  row.AllowedIPs,
		UserID:      row.UserID,
		UserName:    userNames[row.UserID],
		ExpiresAt:   row.ExpiresAt,
		LastUsedAt:  row.LastUsedAt,
		LastUsedIP:  row.LastUsedIP,
		Revoked:     row.Revoked == 1,
		Expired:     row.ExpiresAt > 0 && row.ExpiresAt <= now,
		CreatedTime: row.CreatedTime,
	}
}

func hashAPIToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func generateAPIToken() (string, string, error) {
	buf := make([]byte, 36)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	out := make([]byte, 36)
	for i, b := range buf {
		out[i] = base62Alphabet[int(b)%len(base62Alphabet)]
	}
	raw := apiTokenPrefix + string(out)
	return raw, raw[:10], nil
}

// ValidateAPIToken authenticates an flx_ API token and enforces expiration,
// revocation, creator active admin state, and optional IP/CIDR allowlist.
func (h *Handler) ValidateAPIToken(rawToken string, clientIP net.IP) (*middleware.APITokenIdentity, error) {
	if h == nil || h.repo == nil {
		return nil, middleware.ErrAPITokenInvalid
	}
	rawToken = strings.TrimSpace(rawToken)
	if !strings.HasPrefix(rawToken, apiTokenPrefix) || len(rawToken) < 16 {
		return nil, middleware.ErrAPITokenInvalid
	}
	digest := hashAPIToken(rawToken)
	row, err := h.repo.GetAPITokenByHash(digest)
	if err != nil || row == nil {
		return nil, middleware.ErrAPITokenInvalid
	}
	if subtle.ConstantTimeCompare([]byte(row.TokenHash), []byte(digest)) != 1 {
		return nil, middleware.ErrAPITokenInvalid
	}
	if row.Revoked == 1 {
		return nil, middleware.ErrAPITokenInvalid
	}
	now := time.Now().UnixMilli()
	if row.ExpiresAt > 0 && row.ExpiresAt <= now {
		return nil, middleware.ErrAPITokenInvalid
	}
	if row.Scope != apiTokenScopeReadonly && row.Scope != apiTokenScopeOperator {
		return nil, middleware.ErrAPITokenInvalid
	}
	state, err := h.repo.GetUserAuthState(row.UserID)
	if err != nil || state == nil || state.Status != 1 || state.RoleID != 0 {
		return nil, middleware.ErrAPITokenInvalid
	}
	if strings.TrimSpace(row.AllowedIPs) != "" && !security.IsIPAllowed(clientIP, row.AllowedIPs) {
		return nil, middleware.ErrAPITokenIPForbidden
	}

	userNames, _ := h.repo.UserNames()
	userName := userNames[row.UserID]
	if userName == "" {
		userName = "api-token"
	}
	ipStr := ""
	if clientIP != nil {
		ipStr = clientIP.String()
	}
	if now-row.LastUsedAt >= apiTokenTouchInterval || row.LastUsedIP != ipStr {
		_ = h.repo.TouchAPITokenUsage(row.ID, now, ipStr)
	}

	return &middleware.APITokenIdentity{
		Claims: auth.Claims{
			Sub:    itoa64(row.UserID),
			User:   userName,
			Name:   userName + " (token:" + row.Name + ")",
			RoleID: 0,
		},
		Scope: row.Scope,
	}, nil
}

func itoa64(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func requireWebAdminSession(w http.ResponseWriter, r *http.Request) (int64, bool) {
	if middleware.IsAPITokenRequest(r) {
		response.WriteJSON(w, response.Err(403, "该高危操作禁止使用 API 令牌，请登录网页后台执行"))
		return 0, false
	}
	userID, roleID, err := userRoleFromRequest(r)
	if err != nil {
		response.WriteJSON(w, response.Err(401, "无效的token或token已过期"))
		return 0, false
	}
	if roleID != 0 {
		response.WriteJSON(w, response.Err(403, "权限不足，仅管理员可操作"))
		return 0, false
	}
	return userID, true
}

func (h *Handler) apiTokenList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	if _, ok := requireWebAdminSession(w, r); !ok {
		return
	}
	rows, err := h.repo.ListAPITokens()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	names, _ := h.repo.UserNames()
	now := time.Now().UnixMilli()
	out := make([]apiTokenView, 0, len(rows))
	for _, row := range rows {
		out = append(out, apiTokenToView(row, names, now))
	}
	response.WriteJSON(w, response.OK(out))
}

func (h *Handler) apiTokenCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	userID, ok := requireWebAdminSession(w, r)
	if !ok {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Scope       string `json:"scope"`
		AllowedIPs  string `json:"allowedIps"`
		ExpiresDays int    `json:"expiresDays"`
	}
	if err := decodeJSON(r.Body, &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 100 {
		response.WriteJSON(w, response.ErrDefault("令牌名称不能为空且不超过 100 字"))
		return
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = apiTokenScopeOperator
	}
	if scope != apiTokenScopeReadonly && scope != apiTokenScopeOperator {
		response.WriteJSON(w, response.ErrDefault("令牌权限仅支持 readonly 或 operator"))
		return
	}
	allowedIPs, err := security.NormalizeAllowedIPs(req.AllowedIPs)
	if err != nil {
		response.WriteJSON(w, response.ErrDefault("白名单 IP/CIDR 格式错误: "+err.Error()))
		return
	}
	if req.ExpiresDays < 0 || req.ExpiresDays > 3650 {
		response.WriteJSON(w, response.ErrDefault("有效期天数需在 0–3650 之间（0 表示永不过期）"))
		return
	}

	rawToken, prefix, err := generateAPIToken()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	now := time.Now().UnixMilli()
	var expiresAt int64
	if req.ExpiresDays > 0 {
		expiresAt = now + int64(req.ExpiresDays)*int64(24*time.Hour/time.Millisecond)
	}
	row := &model.APIToken{
		Name:        name,
		Prefix:      prefix,
		TokenHash:   hashAPIToken(rawToken),
		Scope:       scope,
		AllowedIPs:  allowedIPs,
		UserID:      userID,
		ExpiresAt:   expiresAt,
		CreatedTime: now,
	}
	if err := h.repo.CreateAPIToken(row); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	names, _ := h.repo.UserNames()
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"token": rawToken,
		"item":  apiTokenToView(*row, names, now),
	}))
}

func (h *Handler) apiTokenRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	if _, ok := requireWebAdminSession(w, r); !ok {
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	n, err := h.repo.RevokeAPIToken(id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if n == 0 {
		response.WriteJSON(w, response.ErrDefault("令牌不存在"))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) apiTokenDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	if _, ok := requireWebAdminSession(w, r); !ok {
		return
	}
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	n, err := h.repo.DeleteAPIToken(id)
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if n == 0 {
		response.WriteJSON(w, response.ErrDefault("令牌不存在"))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}
