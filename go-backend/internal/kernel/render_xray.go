package kernel

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type obj = map[string]interface{}

const (
	xrayStatsPort = 19850
	xrayStatsAddr = "127.0.0.1:19850"
	xrayAPITag    = "flux-api"
)

func renderXray(inbounds []Inbound, opts RenderOptions) (string, error) {
	list := make([]interface{}, 0, len(inbounds))
	var fallbackRules []interface{}
	for _, in := range inbounds {
		item, err := xrayInbound(in)
		if err != nil {
			return "", fmt.Errorf("入站 %d: %v", in.ID, err)
		}
		list = append(list, item)
		if in.Security.Type == SecurityReality && in.Security.RealityNoSteal && in.Security.RealityFallbackPort > 0 {
			fbItem, fbRules, err := xrayRealityFallback(in)
			if err != nil {
				return "", fmt.Errorf("入站 %d: %v", in.ID, err)
			}
			list = append(list, fbItem)
			fallbackRules = append(fallbackRules, fbRules...)
		}
	}
	if opts.KernelStats {
		list = append(list, obj{
			"tag":      xrayAPITag,
			"listen":   "127.0.0.1",
			"port":     xrayStatsPort,
			"protocol": "dokodemo-door",
			"settings": obj{"address": "127.0.0.1"},
		})
	}
	cfg := obj{
		"log":      obj{"loglevel": "warning"},
		"inbounds": list,
		"outbounds": []interface{}{
			obj{"protocol": "freedom", "tag": "direct"},
			obj{"protocol": "blackhole", "tag": "block"},
		},
	}
	if opts.KernelStats {
		cfg["stats"] = obj{}
		cfg["api"] = obj{
			"tag":      xrayAPITag,
			"services": []string{"StatsService"},
		}
		cfg["policy"] = obj{
			"levels": obj{
				"0": obj{
					"statsUserUplink":   true,
					"statsUserDownlink": true,
				},
			},
		}
	}
	// Chained inbounds leave through their target; their rules come first
	// so their users do not match the direct per-user rules below.
	chained, rules, err := xrayChainRoutes(inbounds)
	if err != nil {
		return "", err
	}
	if len(fallbackRules) > 0 {
		rules = append(fallbackRules, rules...)
	}
	if opts.KernelStats {
		rules = append([]interface{}{
			obj{"type": "field", "inboundTag": []string{xrayAPITag}, "outboundTag": xrayAPITag},
		}, rules...)
	}
	outbounds := append(cfg["outbounds"].([]interface{}), chained...)
	// Each user leaves through a direct outbound whose sockets carry the
	// user's mark (see UserMark).
	for _, id := range markedUsers(inbounds) {
		tag := userOutboundTag(id)
		outbounds = append(outbounds, obj{"protocol": "freedom", "tag": tag,
			"streamSettings": obj{"sockopt": obj{"mark": UserMark(id)}}})
		rules = append(rules, obj{"type": "field", "user": xrayUserMatch(id), "outboundTag": tag})
	}
	cfg["outbounds"] = outbounds
	if len(rules) > 0 {
		cfg["routing"] = obj{"rules": rules}
	}
	if err := applyOutboundIPPolicies(TypeXray, cfg, inbounds, false); err != nil {
		return "", err
	}
	return marshalConfig(cfg)
}

func xrayInbound(in Inbound) (obj, error) {
	settings, err := xraySettings(in)
	if err != nil {
		return nil, err
	}
	item := obj{
		"tag":      in.Tag(),
		"listen":   in.Listen,
		"port":     in.Port,
		"protocol": in.Protocol,
		"settings": settings,
		"sniffing": obj{
			"enabled":      true,
			"destOverride": []string{"http", "tls", "quic"},
		},
	}
	if in.Protocol == ProtocolWireGuard {
		// WireGuard brings its own UDP transport.
	} else if in.Protocol != ProtocolShadowsocks {
		item["streamSettings"] = xrayStream(in)
	} else if in.AcceptProxyProtocol {
		item["streamSettings"] = obj{"network": TransportTCP}
	}
	if in.AcceptProxyProtocol {
		stream := item["streamSettings"].(obj)
		stream["sockopt"] = obj{"acceptProxyProtocol": true}
	}
	if err := mergeExtra(item, in.Extra); err != nil {
		return nil, err
	}
	return item, nil
}

func xraySettings(in Inbound) (obj, error) {
	s := in.Settings
	switch in.Protocol {
	case ProtocolVLESS:
		client := obj{"id": s.UUID}
		if s.Flow != "" {
			client["flow"] = s.Flow
		}
		return obj{"clients": append([]interface{}{client}, xrayUserClients(in)...),
			"decryption": firstNonEmptyString(s.Decryption, "none")}, nil
	case ProtocolVMess:
		return obj{"clients": append([]interface{}{obj{"id": s.UUID}}, xrayUserClients(in)...)}, nil
	case ProtocolTrojan:
		return obj{"clients": append([]interface{}{obj{"password": s.Password}}, xrayUserClients(in)...)}, nil
	case ProtocolShadowsocks:
		if s.SSSubscriptionUsers {
			if SS2022KeyLen(s.Method) > 0 {
				return obj{"method": s.Method, "password": s.Password, "clients": xrayUserClients(in), "network": "tcp,udp"}, nil
			}
			return obj{"users": xrayUserClients(in), "network": "tcp,udp"}, nil
		}
		return obj{"method": s.Method, "password": s.Password, "network": "tcp,udp"}, nil
	case ProtocolSOCKS:
		accounts := append([]interface{}{obj{"user": s.Username, "pass": s.Password}}, xrayUserClients(in)...)
		return obj{"auth": "password", "accounts": accounts, "udp": true}, nil
	case ProtocolHTTP:
		return obj{"accounts": append([]interface{}{obj{"user": s.Username, "pass": s.Password}}, xrayUserClients(in)...)}, nil
	case ProtocolWireGuard:
		return obj{
			"secretKey": s.PrivateKey,
			"peers":     []interface{}{obj{"publicKey": s.PeerPublicKey, "allowedIPs": []string{s.PeerAddress + "/32"}}},
			"mtu":       s.MTU,
		}, nil
	}
	return nil, fmt.Errorf("Xray 不支持协议 %s", in.Protocol)
}

func xrayStream(in Inbound) obj {
	t := in.Transport
	stream := obj{"network": t.Type, "security": in.Security.Type}
	switch t.Type {
	case TransportWS:
		ws := obj{"path": t.Path}
		if t.Host != "" {
			ws["host"] = t.Host
		}
		stream["wsSettings"] = ws
	case TransportGRPC:
		stream["grpcSettings"] = obj{"serviceName": t.ServiceName}
	case TransportXHTTP:
		x := obj{"path": t.Path, "mode": t.Mode}
		if t.Host != "" {
			x["host"] = t.Host
		}
		stream["xhttpSettings"] = x
	}

	sec := in.Security
	switch sec.Type {
	case SecurityTLS:
		tls := obj{"serverName": sec.SNI}
		if len(sec.ALPN) > 0 {
			tls["alpn"] = sec.ALPN
		}
		if sec.CertMode == "file" {
			tls["certificates"] = []interface{}{obj{"certificateFile": sec.CertFile, "keyFile": sec.KeyFile}}
		} else {
			tls["certificates"] = []interface{}{obj{
				"certificate": pemLines(sec.CertPEM),
				"key":         pemLines(sec.KeyPEM),
			}}
		}
		stream["tlsSettings"] = tls
	case SecurityReality:
		dest := sec.RealityDest
		serverNames := []string{sec.SNI}
		if sec.RealityNoSteal && sec.RealityFallbackPort > 0 {
			dest = fmt.Sprintf("127.0.0.1:%d", sec.RealityFallbackPort)
			if len(sec.RealityServerNames) > 0 {
				serverNames = sec.RealityServerNames
			}
		}
		stream["realitySettings"] = obj{
			"show":        false,
			"dest":        dest,
			"xver":        0,
			"serverNames": serverNames,
			"privateKey":  sec.RealityPrivateKey,
			"shortIds":    sec.RealityShortIDs,
		}
		// Inbounds saved before the field existed get the default too.
		minVer := sec.RealityMinClientVer
		if minVer == "" {
			minVer = DefaultRealityMinClientVer
		}
		stream["realitySettings"].(obj)["minClientVer"] = minVer
	}
	return stream
}

func xrayRealityFallback(in Inbound) (obj, []interface{}, error) {
	sec := in.Security
	host, portStr, err := net.SplitHostPort(sec.RealityDest)
	if err != nil {
		return nil, nil, fmt.Errorf("Reality 目标地址错误: %v", err)
	}
	port, _ := strconv.Atoi(portStr)
	fallbackTag := in.Tag() + "-fallback"
	item := obj{
		"tag":      fallbackTag,
		"listen":   "127.0.0.1",
		"port":     sec.RealityFallbackPort,
		"protocol": "dokodemo-door",
		"settings": obj{
			"address": host,
			"port":    port,
			"network": "tcp",
		},
		"sniffing": obj{
			"enabled":      true,
			"destOverride": []string{"tls"},
			"routeOnly":    true,
		},
	}
	names := sec.RealityServerNames
	if len(names) == 0 && sec.SNI != "" {
		names = []string{sec.SNI}
	}
	rules := []interface{}{
		obj{
			"type":        "field",
			"inboundTag":  []string{fallbackTag},
			"domain":      names,
			"outboundTag": "direct",
		},
		obj{
			"type":        "field",
			"inboundTag":  []string{fallbackTag},
			"outboundTag": "block",
		},
	}
	return item, rules, nil
}

// pemLines splits a PEM block into the line array form Xray accepts inline.
func pemLines(pem string) []string {
	lines := strings.Split(strings.TrimSpace(pem), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
