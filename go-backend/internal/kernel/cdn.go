package kernel

import (
	"errors"
	"strings"
)

// CFHTTPSPorts and CFHTTPPorts are the standard ports proxied by Cloudflare's
// orange-cloud CDN on free and paid plans.
var (
	CFHTTPSPorts = []int{443, 2053, 2083, 2087, 2096, 8443}
	CFHTTPPorts  = []int{80, 8080, 8880, 2052, 2082, 2086, 2095}
)

// DefaultCFOriginIPv4CIDRs and DefaultCFOriginIPv6CIDRs are fallback snapshots
// of Cloudflare's published origin IP ranges (https://www.cloudflare.com/ips-v4
// and ips-v6) used when the live fetch fails.
var DefaultCFOriginIPv4CIDRs = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
}

var DefaultCFOriginIPv6CIDRs = []string{
	"2400:cb00::/32",
	"2606:4700::/32",
	"2803:f800::/32",
	"2405:b500::/32",
	"2405:8100::/32",
	"2a06:98c0::/29",
	"2c0f:f248::/32",
}

const maxCDNCustomEntriesBytes = 64 * 1024

// CDNConfig describes how a panel-managed inbound is exposed behind
// Cloudflare's CDN proxy (orange cloud) and expanded into optimal edge
// endpoints when generating share links and subscriptions.
type CDNConfig struct {
	Enabled        bool   `json:"enabled"`
	Domain         string `json:"domain,omitempty"`
	EdgePort       int    `json:"edgePort,omitempty"`
	EdgeTLS        *bool  `json:"edgeTls,omitempty"`
	PoolID         int64  `json:"poolId,omitempty"`
	CustomEntries  string `json:"customEntries,omitempty"`
	KeepOriginHost bool   `json:"keepOriginHost,omitempty"`
	OnlyCFOrigin   bool   `json:"onlyCfOrigin,omitempty"`
}

func IsCFHTTPSPort(port int) bool {
	for _, p := range CFHTTPSPorts {
		if p == port {
			return true
		}
	}
	return false
}

func IsCFHTTPPort(port int) bool {
	for _, p := range CFHTTPPorts {
		if p == port {
			return true
		}
	}
	return false
}

func IsCFProxyPort(port int) bool {
	return IsCFHTTPSPort(port) || IsCFHTTPPort(port)
}

// CDNSupported reports whether in uses a protocol, transport, and security
// combination that can traverse Cloudflare's HTTP/HTTPS CDN proxy.
func CDNSupported(in Inbound) error {
	switch in.Protocol {
	case ProtocolVLESS, ProtocolVMess, ProtocolTrojan:
	default:
		return errors.New("Cloudflare CDN 仅支持 VLESS、VMess 或 Trojan 协议")
	}
	switch in.Transport.Type {
	case TransportWS, TransportGRPC, TransportXHTTP:
	default:
		return errors.New("Cloudflare CDN 仅支持 WebSocket、gRPC 或 XHTTP 传输方式")
	}
	switch in.Security.Type {
	case SecurityTLS, SecurityNone:
	default:
		return errors.New("Cloudflare CDN 不支持 Reality 或 ShadowTLS，请使用 TLS 或无加密（配合 CF Flexible）")
	}
	return nil
}

// NormalizeCDNConfig validates and normalizes cfg against in.
func NormalizeCDNConfig(in Inbound, cfg *CDNConfig) error {
	if cfg == nil || !cfg.Enabled {
		if cfg != nil {
			*cfg = CDNConfig{}
		}
		return nil
	}
	if err := CDNSupported(in); err != nil {
		return err
	}
	cfg.Domain = strings.ToLower(strings.TrimSpace(cfg.Domain))
	if cfg.Domain == "" {
		cfg.Domain = strings.ToLower(strings.TrimSpace(firstNonEmptyString(in.Security.SNI, in.Transport.Host)))
	}
	if cfg.Domain == "" {
		return errors.New("开启 Cloudflare CDN 需要填写已开启橙色云的域名")
	}
	if !strings.Contains(cfg.Domain, ".") || !hostPattern.MatchString(cfg.Domain) {
		return errors.New("Cloudflare CDN 域名格式错误")
	}

	if cfg.EdgePort == 0 {
		if IsCFProxyPort(in.Port) {
			cfg.EdgePort = in.Port
		} else {
			cfg.EdgePort = 443
		}
	}
	if cfg.EdgePort < 1 || cfg.EdgePort > 65535 {
		return errors.New("Cloudflare 边缘端口必须在 1-65535 之间")
	}
	if in.Transport.Type == TransportGRPC && !cfg.EffectiveEdgeTLS(cfg.EdgePort) {
		return errors.New("gRPC 走 Cloudflare CDN 时边缘端口必须开启 TLS")
	}
	cfg.CustomEntries = strings.TrimSpace(cfg.CustomEntries)
	if len(cfg.CustomEntries) > maxCDNCustomEntriesBytes {
		return errors.New("自定义优选列表过大")
	}
	return nil
}

// EffectiveEdgePort returns the port clients dial on Cloudflare edge nodes.
func (c CDNConfig) EffectiveEdgePort(inboundPort int) int {
	if c.EdgePort > 0 && c.EdgePort <= 65535 {
		return c.EdgePort
	}
	if IsCFProxyPort(inboundPort) {
		return inboundPort
	}
	return 443
}

// EffectiveEdgeTLS reports whether clients use TLS when connecting to port on
// Cloudflare edge nodes.
func (c CDNConfig) EffectiveEdgeTLS(port int) bool {
	if c.EdgeTLS != nil {
		return *c.EdgeTLS
	}
	return !IsCFHTTPPort(port)
}

// ApplyCDNToEndpoint clones base for one Cloudflare edge host/port, enforcing
// the CDN domain as WS/XHTTP Host header and TLS SNI, setting matching ALPN,
// and marking TrustedEdgeTLS so clients verify Cloudflare's edge certificate.
func ApplyCDNToEndpoint(base Endpoint, cfg CDNConfig, host string, port int, name string) Endpoint {
	e := base
	e.Name = name
	e.Host = strings.Trim(strings.TrimSpace(host), "[]")
	if port <= 0 || port > 65535 {
		port = cfg.EffectiveEdgePort(base.Port)
	}
	e.Port = port
	e.Inbound.Transport.Host = cfg.Domain

	if cfg.EffectiveEdgeTLS(port) {
		e.Inbound.Security.Type = SecurityTLS
		e.Inbound.Security.SNI = cfg.Domain
		if e.Inbound.Security.Fingerprint == "" {
			e.Inbound.Security.Fingerprint = DefaultFingerprint
		}
		if len(e.Inbound.Security.ALPN) == 0 {
			switch e.Inbound.Transport.Type {
			case TransportWS:
				e.Inbound.Security.ALPN = []string{"http/1.1"}
			case TransportGRPC:
				e.Inbound.Security.ALPN = []string{"h2"}
			case TransportXHTTP:
				e.Inbound.Security.ALPN = []string{"h2", "http/1.1"}
			}
		}
		e.TrustedEdgeTLS = true
	} else {
		e.Inbound.Security.Type = SecurityNone
		e.Inbound.Security.SNI = ""
		e.Inbound.Security.ALPN = nil
		e.TrustedEdgeTLS = false
	}
	return e
}
