package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"go-backend/internal/http/response"
	"go-backend/internal/kernel"
	"go-backend/internal/store/model"
)

const (
	maxCFPoolFetchBytes = 2 << 20
	cfPoolCheckInterval = 10 * time.Minute
)

var (
	cfPoolMu sync.Mutex

	cfOriginCIDRCache = struct {
		mu    sync.Mutex
		at    time.Time
		cidrs []string
	}{}
)

type cfPoolView struct {
	ID            int64                `json:"id"`
	Name          string               `json:"name"`
	Kind          string               `json:"kind"`
	URL           string               `json:"url"`
	RawText       string               `json:"rawText"`
	UserAgent     string               `json:"userAgent"`
	IntervalHours int                  `json:"intervalHours"`
	DefaultPort   int                  `json:"defaultPort"`
	MaxEntries    int                  `json:"maxEntries"`
	IPFilter      string               `json:"ipFilter"`
	Entries       []kernel.CFPoolEntry `json:"entries"`
	EntryCount    int                  `json:"entryCount"`
	Enabled       bool                 `json:"enabled"`
	LastFetch     int64                `json:"lastFetch"`
	LastError     string               `json:"lastError"`
	CreatedTime   int64                `json:"createdTime"`
	UpdatedTime   int64                `json:"updatedTime"`
}

func parseCachedCFEntries(raw string) []kernel.CFPoolEntry {
	if strings.TrimSpace(raw) == "" {
		return []kernel.CFPoolEntry{}
	}
	var out []kernel.CFPoolEntry
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []kernel.CFPoolEntry{}
	}
	return out
}

func cfPoolViewOf(p *model.CFPool) cfPoolView {
	entries := parseCachedCFEntries(p.CachedEntries)
	return cfPoolView{
		ID:            p.ID,
		Name:          p.Name,
		Kind:          p.Kind,
		URL:           p.URL,
		RawText:       p.RawText,
		UserAgent:     p.UserAgent,
		IntervalHours: p.IntervalHours,
		DefaultPort:   p.DefaultPort,
		MaxEntries:    p.MaxEntries,
		IPFilter:      p.IPFilter,
		Entries:       entries,
		EntryCount:    p.EntryCount,
		Enabled:       p.Enabled == 1,
		LastFetch:     p.LastFetch,
		LastError:     p.LastError,
		CreatedTime:   p.CreatedTime,
		UpdatedTime:   p.UpdatedTime,
	}
}

func normalizeCFPoolFilter(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case model.CFPoolFilterIPv4:
		return model.CFPoolFilterIPv4
	case model.CFPoolFilterIPv6:
		return model.CFPoolFilterIPv6
	case model.CFPoolFilterDomain:
		return model.CFPoolFilterDomain
	default:
		return model.CFPoolFilterAll
	}
}

func (h *Handler) cfPoolList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	pools, err := h.repo.ListCFPools()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	out := make([]cfPoolView, 0, len(pools))
	for i := range pools {
		out = append(out, cfPoolViewOf(&pools[i]))
	}
	response.WriteJSON(w, response.OK(out))
}

func (h *Handler) cfPoolParsePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		RawText     string `json:"rawText"`
		DefaultPort int    `json:"defaultPort"`
		MaxEntries  int    `json:"maxEntries"`
		IPFilter    string `json:"ipFilter"`
	}
	if err := decodeJSON(http.MaxBytesReader(w, r.Body, maxCFPoolFetchBytes), &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	defaultPort := req.DefaultPort
	if defaultPort <= 0 || defaultPort > 65535 {
		defaultPort = 443
	}
	maxEntries := req.MaxEntries
	if maxEntries <= 0 || maxEntries > 50 {
		maxEntries = 10
	}
	entries := kernel.ParseCFEntries(req.RawText, defaultPort, normalizeCFPoolFilter(req.IPFilter), maxEntries)
	response.WriteJSON(w, response.OK(map[string]interface{}{
		"entries": entries,
		"count":   len(entries),
	}))
}

func (h *Handler) cfPoolSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		Kind          string `json:"kind"`
		URL           string `json:"url"`
		RawText       string `json:"rawText"`
		UserAgent     string `json:"userAgent"`
		IntervalHours int    `json:"intervalHours"`
		DefaultPort   int    `json:"defaultPort"`
		MaxEntries    int    `json:"maxEntries"`
		IPFilter      string `json:"ipFilter"`
		Enabled       *bool  `json:"enabled"`
	}
	if err := decodeJSON(http.MaxBytesReader(w, r.Body, maxCFPoolFetchBytes), &req); err != nil {
		response.WriteJSON(w, response.ErrDefault("请求参数错误或内容过大"))
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimSpace(req.URL)
	req.UserAgent = strings.TrimSpace(req.UserAgent)
	if req.Name == "" || len([]rune(req.Name)) > 100 {
		response.WriteJSON(w, response.ErrDefault("请填写名称（最多 100 个字）"))
		return
	}
	kind := model.CFPoolKindManual
	if strings.EqualFold(strings.TrimSpace(req.Kind), model.CFPoolKindURL) {
		kind = model.CFPoolKindURL
	}
	if kind == model.CFPoolKindURL {
		if !isSubscriptionURL(req.URL) {
			response.WriteJSON(w, response.ErrDefault("请填写 http(s) 优选源地址"))
			return
		}
		if len(req.UserAgent) > 255 || strings.ContainsFunc(req.UserAgent, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
			response.WriteJSON(w, response.ErrDefault("User-Agent 不正确（一行文字，最多 255 个字符）"))
			return
		}
	} else {
		req.URL = ""
		req.UserAgent = ""
		req.IntervalHours = 0
		if strings.TrimSpace(req.RawText) == "" {
			response.WriteJSON(w, response.ErrDefault("请填写优选 IP 或优选域名列表"))
			return
		}
	}
	if req.IntervalHours < 0 || req.IntervalHours > 24*7 {
		response.WriteJSON(w, response.ErrDefault("更新间隔需在 0-168 小时之间"))
		return
	}
	defaultPort := req.DefaultPort
	if defaultPort == 0 {
		defaultPort = 443
	}
	if defaultPort < 1 || defaultPort > 65535 {
		response.WriteJSON(w, response.ErrDefault("默认端口必须在 1-65535 之间"))
		return
	}
	maxEntries := req.MaxEntries
	if maxEntries == 0 {
		maxEntries = 10
	}
	if maxEntries < 1 || maxEntries > 50 {
		response.WriteJSON(w, response.ErrDefault("最多保留条数需在 1-50 之间"))
		return
	}
	ipFilter := normalizeCFPoolFilter(req.IPFilter)

	now := time.Now().UnixMilli()
	pool := &model.CFPool{CreatedTime: now, Enabled: 1, CachedEntries: "[]"}
	if req.ID > 0 {
		existing, err := h.repo.GetCFPool(req.ID)
		if err != nil || existing == nil {
			response.WriteJSON(w, response.ErrDefault("CF 优选池不存在"))
			return
		}
		pool = existing
	}

	needSync := pool.ID == 0 ||
		pool.Kind != kind ||
		pool.URL != req.URL ||
		pool.RawText != req.RawText ||
		pool.UserAgent != req.UserAgent ||
		pool.DefaultPort != defaultPort ||
		pool.MaxEntries != maxEntries ||
		pool.IPFilter != ipFilter

	pool.Name = req.Name
	pool.Kind = kind
	pool.URL = req.URL
	pool.RawText = req.RawText
	pool.UserAgent = req.UserAgent
	pool.IntervalHours = req.IntervalHours
	pool.DefaultPort = defaultPort
	pool.MaxEntries = maxEntries
	pool.IPFilter = ipFilter
	if req.Enabled != nil {
		pool.Enabled = boolToInt(*req.Enabled)
	}
	pool.UpdatedTime = now

	if kind == model.CFPoolKindManual {
		entries := kernel.ParseCFEntries(pool.RawText, pool.DefaultPort, pool.IPFilter, pool.MaxEntries)
		if len(entries) == 0 {
			response.WriteJSON(w, response.ErrDefault("未解析出任何有效的 IP 或域名，请检查格式"))
			return
		}
		encoded, err := json.Marshal(entries)
		if err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		pool.CachedEntries = string(encoded)
		pool.EntryCount = len(entries)
		pool.LastFetch = now
		pool.LastError = ""
		if err := h.repo.SaveCFPool(pool); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		if err := h.repo.UpdateCFPoolStatus(pool.ID, map[string]interface{}{
			"cached_entries": pool.CachedEntries,
			"entry_count":    pool.EntryCount,
			"last_fetch":     pool.LastFetch,
			"last_error":     "",
		}); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
		saved, _ := h.repo.GetCFPool(pool.ID)
		if saved == nil {
			saved = pool
		}
		response.WriteJSON(w, response.OK(cfPoolViewOf(saved)))
		return
	}

	if err := h.repo.SaveCFPool(pool); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	if !needSync {
		saved, _ := h.repo.GetCFPool(pool.ID)
		if saved == nil {
			saved = pool
		}
		response.WriteJSON(w, response.OK(cfPoolViewOf(saved)))
		return
	}
	h.respondCFPoolSync(w, r.Context(), pool)
}

func (h *Handler) respondCFPoolSync(w http.ResponseWriter, ctx context.Context, pool *model.CFPool) {
	err := h.syncCFPool(ctx, pool)
	saved, _ := h.repo.GetCFPool(pool.ID)
	if saved == nil {
		saved = pool
	}
	view := cfPoolViewOf(saved)
	if err != nil {
		response.WriteJSON(w, response.R{Code: -1, Msg: err.Error(), Data: view, TS: time.Now().UnixMilli()})
		return
	}
	response.WriteJSON(w, response.R{
		Code: 0,
		Msg:  fmt.Sprintf("已同步 %d 条优选地址", view.EntryCount),
		Data: view,
		TS:   time.Now().UnixMilli(),
	})
}

func (h *Handler) cfPoolRefresh(w http.ResponseWriter, r *http.Request) {
	id := idFromBody(r, w)
	if id <= 0 {
		return
	}
	pool, err := h.repo.GetCFPool(id)
	if err != nil || pool == nil {
		response.WriteJSON(w, response.ErrDefault("CF 优选池不存在"))
		return
	}
	if pool.Kind != model.CFPoolKindURL {
		entries := kernel.ParseCFEntries(pool.RawText, pool.DefaultPort, pool.IPFilter, pool.MaxEntries)
		if len(entries) == 0 {
			response.WriteJSON(w, response.ErrDefault("未解析出任何有效的 IP 或域名"))
			return
		}
		encoded, _ := json.Marshal(entries)
		now := time.Now().UnixMilli()
		_ = h.repo.UpdateCFPoolStatus(pool.ID, map[string]interface{}{
			"cached_entries": string(encoded),
			"entry_count":    len(entries),
			"last_fetch":     now,
			"last_error":     "",
		})
		saved, _ := h.repo.GetCFPool(pool.ID)
		if saved == nil {
			saved = pool
		}
		response.WriteJSON(w, response.OK(cfPoolViewOf(saved)))
		return
	}
	h.respondCFPoolSync(w, r.Context(), pool)
}

func (h *Handler) cfPoolDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteJSON(w, response.ErrDefault("请求失败"))
		return
	}
	var req struct {
		ID    int64 `json:"id"`
		Force bool  `json:"force"`
	}
	if err := decodeJSON(r.Body, &req); err != nil || req.ID <= 0 {
		response.WriteJSON(w, response.ErrDefault("请求参数错误"))
		return
	}
	rows, err := h.repo.ListNodeInbounds()
	if err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	var refs []model.NodeInbound
	var refNames []string
	for i := range rows {
		cdn := rowCDNConfig(rows[i])
		if cdn != nil && cdn.Enabled && cdn.PoolID == req.ID {
			refs = append(refs, rows[i])
			refNames = append(refNames, rows[i].Name)
		}
	}
	if len(refs) > 0 && !req.Force {
		response.WriteJSON(w, response.ErrDefault("该优选池正被入站使用："+strings.Join(refNames, "、")+"（请先解绑或强制删除）"))
		return
	}
	now := time.Now().UnixMilli()
	for i := range refs {
		cdn := rowCDNConfig(refs[i])
		if cdn == nil {
			continue
		}
		cdn.PoolID = 0
		refs[i].CDNConfig = encodeCDNConfig(cdn)
		refs[i].UpdatedTime = now
		if err := h.repo.SaveNodeInbound(&refs[i]); err != nil {
			response.WriteJSON(w, response.Err(-2, err.Error()))
			return
		}
	}
	if err := h.repo.DeleteCFPool(req.ID); err != nil {
		response.WriteJSON(w, response.Err(-2, err.Error()))
		return
	}
	response.WriteJSON(w, response.OKEmpty())
}

func (h *Handler) syncCFPool(ctx context.Context, pool *model.CFPool) error {
	cfPoolMu.Lock()
	defer cfPoolMu.Unlock()

	now := time.Now().UnixMilli()
	fail := func(err error) error {
		// Keep previous cached_entries and entry_count when a refresh fails so
		// existing subscriptions do not lose endpoints on transient upstream errors.
		_ = h.repo.UpdateCFPoolStatus(pool.ID, map[string]interface{}{
			"last_fetch": now,
			"last_error": truncateRunes(err.Error(), 500),
		})
		return err
	}

	reqCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, pool.URL, nil)
	if err != nil {
		return fail(fmt.Errorf("构造请求失败：%v", err))
	}
	ua := strings.TrimSpace(pool.UserAgent)
	if ua == "" {
		ua = defaultFetchUserAgent
	}
	req.Header.Set("User-Agent", ua)
	resp, err := fetchClient(h.fetchAllowLocal(), 25*time.Second).Do(req)
	if err != nil {
		return fail(fmt.Errorf("拉取失败：%v", fetchErr(err)))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("拉取失败：HTTP %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCFPoolFetchBytes+1))
	if err != nil {
		return fail(fmt.Errorf("读取响应失败：%v", err))
	}
	if len(body) > maxCFPoolFetchBytes {
		return fail(errors.New("优选源响应超过 2MB"))
	}

	entries := kernel.ParseCFEntries(string(body), pool.DefaultPort, pool.IPFilter, pool.MaxEntries)
	if len(entries) == 0 {
		return fail(errors.New("优选源没有返回有效 IP 或域名，已保留上次缓存"))
	}
	encoded, err := json.Marshal(entries)
	if err != nil {
		return fail(err)
	}
	if err := h.repo.UpdateCFPoolStatus(pool.ID, map[string]interface{}{
		"cached_entries": string(encoded),
		"entry_count":    len(entries),
		"last_fetch":     now,
		"last_error":     "",
	}); err != nil {
		return err
	}
	return nil
}

func cfPoolDue(p *model.CFPool, now time.Time) bool {
	if p.Enabled != 1 || p.Kind != model.CFPoolKindURL || p.IntervalHours <= 0 {
		return false
	}
	return now.Sub(time.UnixMilli(p.LastFetch)) >= time.Duration(p.IntervalHours)*time.Hour
}

func (h *Handler) refreshDueCFPools(ctx context.Context, now time.Time) {
	pools, err := h.repo.ListCFPools()
	if err != nil {
		return
	}
	for i := range pools {
		p := &pools[i]
		if !cfPoolDue(p, now) {
			continue
		}
		if err := h.syncCFPool(ctx, p); err != nil {
			log.Printf("cf pool %d refresh failed: %v", p.ID, err)
		}
	}
}

func (h *Handler) runCFPoolLoop(ctx context.Context) {
	defer h.jobsWG.Done()
	ticker := time.NewTicker(cfPoolCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		h.refreshDueCFPools(ctx, time.Now())
	}
}

func (h *Handler) cloudflareOriginCIDRs() []string {
	cfOriginCIDRCache.mu.Lock()
	if len(cfOriginCIDRCache.cidrs) > 0 && time.Since(cfOriginCIDRCache.at) < 24*time.Hour {
		out := append([]string(nil), cfOriginCIDRCache.cidrs...)
		cfOriginCIDRCache.mu.Unlock()
		return out
	}
	cfOriginCIDRCache.mu.Unlock()

	client := fetchClient(false, 8*time.Second)
	fetchCIDRs := func(rawURL string, fallback []string) []string {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return fallback
		}
		req.Header.Set("User-Agent", defaultFetchUserAgent)
		resp, err := client.Do(req)
		if err != nil {
			return fallback
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fallback
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
		if err != nil {
			return fallback
		}
		var out []string
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if p, err := netip.ParsePrefix(line); err == nil {
				out = append(out, p.Masked().String())
			}
		}
		if len(out) == 0 {
			return fallback
		}
		return out
	}

	v4 := fetchCIDRs("https://www.cloudflare.com/ips-v4", kernel.DefaultCFOriginIPv4CIDRs)
	v6 := fetchCIDRs("https://www.cloudflare.com/ips-v6", kernel.DefaultCFOriginIPv6CIDRs)
	merged := make([]string, 0, len(v4)+len(v6))
	merged = append(merged, v4...)
	merged = append(merged, v6...)

	cfOriginCIDRCache.mu.Lock()
	cfOriginCIDRCache.at = time.Now()
	cfOriginCIDRCache.cidrs = append([]string(nil), merged...)
	cfOriginCIDRCache.mu.Unlock()
	return merged
}
