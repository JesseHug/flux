package kernel

import (
	"encoding/csv"
	"encoding/json"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// CFPoolEntry is one optimal Cloudflare edge IP or domain parsed from a pool.
type CFPoolEntry struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Remark string `json:"remark,omitempty"`
}

// ParseCFEntries extracts deduplicated Cloudflare edge entries from raw text
// (JSON arrays/objects such as WeTest/HostMonit, CloudflareSpeedTest CSV, or
// line-oriented TXT such as 776771.xyz / IP:port#remark / optimal domains).
func ParseCFEntries(raw string, defaultPort int, ipFilter string, maxEntries int) []CFPoolEntry {
	if defaultPort <= 0 || defaultPort > 65535 {
		defaultPort = 443
	}
	filter := strings.ToLower(strings.TrimSpace(ipFilter))
	if filter == "" {
		filter = "all"
	}

	var candidates []CFPoolEntry
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []CFPoolEntry{}
	}

	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		if parsed, ok := parseCFJSON(trimmed, defaultPort); ok {
			candidates = parsed
		}
	}
	if len(candidates) == 0 && looksLikeCFCSV(trimmed) {
		candidates = parseCFCSV(trimmed, defaultPort)
	}
	if len(candidates) == 0 {
		candidates = parseCFLines(trimmed, defaultPort)
	}

	out := make([]CFPoolEntry, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		host, ok := normalizeCFHost(c.Host)
		if !ok {
			continue
		}
		if !matchesCFFilter(host, filter) {
			continue
		}
		port := c.Port
		if port <= 0 || port > 65535 {
			port = defaultPort
		}
		key := strings.ToLower(host) + ":" + strconv.Itoa(port)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, CFPoolEntry{
			Host:   host,
			Port:   port,
			Remark: strings.TrimSpace(c.Remark),
		})
		if maxEntries > 0 && len(out) >= maxEntries {
			break
		}
	}
	return out
}

func normalizeCFHost(raw string) (string, bool) {
	h := strings.TrimSpace(raw)
	if h == "" || strings.Contains(h, "://") || strings.ContainsAny(h, "/\\ \t\r\n") {
		return "", false
	}
	h = strings.Trim(h, "[]")
	if addr, err := netip.ParseAddr(h); err == nil {
		if addr.Is4In6() {
			addr = netip.AddrFrom4(addr.As4())
		}
		return addr.String(), true
	}
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	if !strings.Contains(h, ".") || !hostPattern.MatchString(h) || strings.HasPrefix(h, "-") || strings.HasSuffix(h, "-") {
		return "", false
	}
	// Reject bare decimal numbers or trailing-dotless numeric labels like 1.2.3.999.
	allDigitsDots := true
	for _, r := range h {
		if (r < '0' || r > '9') && r != '.' {
			allDigitsDots = false
			break
		}
	}
	if allDigitsDots {
		return "", false
	}
	return h, true
}

func matchesCFFilter(host, filter string) bool {
	addr, err := netip.ParseAddr(host)
	switch filter {
	case "ipv4":
		return err == nil && addr.Is4()
	case "ipv6":
		return err == nil && addr.Is6()
	case "domain":
		return err != nil
	default:
		return true
	}
}

func parseCFJSON(raw string, defaultPort int) ([]CFPoolEntry, bool) {
	var v interface{}
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, false
	}
	var out []CFPoolEntry
	var walk func(node interface{}, groupHint string)
	walk = func(node interface{}, groupHint string) {
		switch t := node.(type) {
		case []interface{}:
			for _, item := range t {
				walk(item, groupHint)
			}
		case map[string]interface{}:
			if host := jsonFirstString(t, "ip", "host", "address", "addr", "domain", "server"); host != "" {
				port := jsonFirstPort(t, "port")
				if port == 0 {
					port = defaultPort
				}
				remark := jsonFirstString(t, "remark", "name", "colo", "line", "line_name", "isp", "region", "node")
				if remark == "" {
					remark = groupHint
				}
				out = append(out, CFPoolEntry{Host: host, Port: port, Remark: remark})
				return
			}
			visited := make(map[string]bool)
			for _, key := range []string{"data", "info", "ips", "nodes", "list", "results", "CM", "CU", "CT", "cm", "cu", "ct"} {
				if sub, ok := t[key]; ok {
					visited[key] = true
					hint := groupHint
					if hint == "" && (key == "CM" || key == "CU" || key == "CT" || key == "cm" || key == "cu" || key == "ct") {
						hint = strings.ToUpper(key)
					}
					walk(sub, hint)
				}
			}
			// Handle arbitrary carrier/CNAME/region map keys deterministically.
			var extraKeys []string
			for k := range t {
				if !visited[k] {
					extraKeys = append(extraKeys, k)
				}
			}
			sort.Strings(extraKeys)
			for _, k := range extraKeys {
				sub := t[k]
				switch sub.(type) {
				case []interface{}, map[string]interface{}:
					walk(sub, k)
				}
			}
		}
	}
	walk(v, "")
	return out, len(out) > 0
}

func jsonFirstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		for mk, mv := range m {
			if strings.EqualFold(mk, k) {
				if s, ok := mv.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
	}
	return ""
}

func jsonFirstPort(m map[string]interface{}, keys ...string) int {
	for _, k := range keys {
		for mk, mv := range m {
			if !strings.EqualFold(mk, k) {
				continue
			}
			switch v := mv.(type) {
			case float64:
				if int(v) > 0 && int(v) <= 65535 {
					return int(v)
				}
			case string:
				if p, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && p > 0 && p <= 65535 {
					return p
				}
			}
		}
	}
	return 0
}

func looksLikeCFCSV(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Count(line, ",") >= 2 {
			return true
		}
		return false
	}
	return false
}

func parseCFCSV(raw string, defaultPort int) []CFPoolEntry {
	r := csv.NewReader(strings.NewReader(raw))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil
	}
	var out []CFPoolEntry
	for idx, rec := range records {
		if len(rec) == 0 {
			continue
		}
		first := strings.TrimSpace(strings.TrimPrefix(rec[0], "\ufeff"))
		if first == "" {
			continue
		}
		if idx == 0 {
			lower := strings.ToLower(first)
			if strings.Contains(lower, "ip") || strings.Contains(lower, "address") || strings.Contains(lower, "host") {
				continue
			}
		}
		host, port := splitHostMaybePort(first, defaultPort)
		var remarkParts []string
		for i := 1; i < len(rec); i++ {
			col := strings.TrimSpace(rec[i])
			if col == "" {
				continue
			}
			// Keep latency/speed/colo columns short when present (e.g. CloudflareSpeedTest).
			if len(remarkParts) < 2 && (strings.Contains(strings.ToLower(col), "ms") ||
				strings.Contains(strings.ToLower(col), "mb") ||
				len([]rune(col)) <= 16) {
				remarkParts = append(remarkParts, col)
			}
		}
		out = append(out, CFPoolEntry{
			Host:   host,
			Port:   port,
			Remark: strings.Join(remarkParts, " "),
		})
	}
	return out
}

func parseCFLines(raw string, defaultPort int) []CFPoolEntry {
	var out []CFPoolEntry
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		remark := ""
		if idx := strings.Index(line, "#"); idx >= 0 {
			remark = strings.TrimSpace(line[idx+1:])
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}
		// Handle whitespace-separated "host port remark..." as well as single "host[:port]".
		fields := strings.Fields(line)
		hostToken := fields[0]
		port := defaultPort
		if len(fields) >= 2 {
			if p, err := strconv.Atoi(fields[1]); err == nil && p > 0 && p <= 65535 {
				port = p
				if remark == "" && len(fields) > 2 {
					remark = strings.Join(fields[2:], " ")
				}
			} else if remark == "" {
				remark = strings.Join(fields[1:], " ")
			}
		}
		host, parsedPort := splitHostMaybePort(hostToken, port)
		out = append(out, CFPoolEntry{
			Host:   host,
			Port:   parsedPort,
			Remark: remark,
		})
	}
	return out
}

func splitHostMaybePort(token string, fallbackPort int) (string, int) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fallbackPort
	}
	// Bracketed IPv6, e.g. [2606:4700::1]:443 or [2606:4700::1]
	if strings.HasPrefix(token, "[") {
		if end := strings.Index(token, "]"); end > 0 {
			host := token[1:end]
			rest := strings.TrimSpace(token[end+1:])
			if strings.HasPrefix(rest, ":") {
				if p, err := strconv.Atoi(rest[1:]); err == nil && p > 0 && p <= 65535 {
					return host, p
				}
			}
			return host, fallbackPort
		}
	}
	// Bare IPv6 literal without port.
	if addr, err := netip.ParseAddr(token); err == nil && addr.Is6() {
		return addr.String(), fallbackPort
	}
	// host:port (single colon, not IPv6).
	if strings.Count(token, ":") == 1 {
		idx := strings.Index(token, ":")
		if p, err := strconv.Atoi(token[idx+1:]); err == nil && p > 0 && p <= 65535 {
			return token[:idx], p
		}
	}
	return token, fallbackPort
}
