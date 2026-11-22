package security

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ResolveClientIP extracts the client IP from r, honoring forwarding headers
// only when the immediate peer is a loopback, private, or link-local proxy.
func ResolveClientIP(r *http.Request) net.IP {
	if r == nil {
		return nil
	}
	remoteIP := ParseIPLiteral(r.RemoteAddr)
	if IsTrustedProxyIP(remoteIP) {
		if ip := ParseForwardedFor(r.Header.Get("X-Forwarded-For")); ip != nil {
			return ip
		}
		if ip := ParseIPLiteral(r.Header.Get("X-Real-IP")); ip != nil {
			return ip
		}
	}
	return remoteIP
}

func ParseForwardedFor(raw string) net.IP {
	for _, part := range strings.Split(raw, ",") {
		if ip := ParseIPLiteral(part); ip != nil {
			return ip
		}
	}
	return nil
}

func ParseIPLiteral(raw string) net.IP {
	value := strings.Trim(strings.TrimSpace(raw), "\"")
	if value == "" {
		return nil
	}
	if ip := net.ParseIP(value); ip != nil {
		return NormalizeIPAddress(ip)
	}
	host, _, err := net.SplitHostPort(value)
	if err != nil {
		return nil
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "" {
		return nil
	}
	return NormalizeIPAddress(net.ParseIP(host))
}

func NormalizeIPAddress(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip.To16()
}

func IsTrustedProxyIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// NormalizeAllowedIPs validates and deduplicates a comma-separated list of IPs or CIDRs.
func NormalizeAllowedIPs(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parts := strings.Split(raw, ",")
	normalized := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			_, network, err := net.ParseCIDR(item)
			if err != nil {
				return "", fmt.Errorf("Invalid allowed IP or CIDR: %s", item)
			}
			item = network.String()
		} else {
			ip := ParseIPLiteral(item)
			if ip == nil {
				return "", fmt.Errorf("Invalid allowed IP or CIDR: %s", item)
			}
			item = ip.String()
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	return strings.Join(normalized, ","), nil
}

// IsIPAllowed checks whether clientIP matches at least one entry in whitelist.
func IsIPAllowed(clientIP net.IP, whitelist string) bool {
	if clientIP == nil {
		return false
	}
	for _, part := range strings.Split(whitelist, ",") {
		entry := strings.TrimSpace(part)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			_, network, err := net.ParseCIDR(entry)
			if err != nil {
				continue
			}
			if network.Contains(clientIP) {
				return true
			}
			continue
		}
		allowedIP := ParseIPLiteral(entry)
		if allowedIP != nil && allowedIP.Equal(clientIP) {
			return true
		}
	}
	return false
}
