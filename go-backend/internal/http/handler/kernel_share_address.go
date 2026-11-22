package handler

import (
	"net/netip"
	"strings"

	"go-backend/internal/store/model"
)

const dualStackV6Suffix = " · v6"

type inboundShareEntrySpec struct {
	Name string
	Host string
}

func inboundShareMode(row *model.NodeInbound) string {
	mode := strings.TrimSpace(row.ShareAddressMode)
	if (mode == "" || mode == "auto") && strings.TrimSpace(row.ShareHost) != "" {
		return "custom" // Addresses saved before this setting keep their override.
	}
	if mode == "" {
		return "auto"
	}
	return mode
}

func nodeShareIP(node *model.Node, ipv6 bool) string {
	if node == nil {
		return ""
	}
	value := node.ServerIPV4.String
	if ipv6 {
		value = node.ServerIPV6.String
	}
	for _, host := range []string{value, node.ServerIP} {
		addr, err := netip.ParseAddr(strings.Trim(strings.TrimSpace(host), "[]"))
		if err == nil && addr.Is6() == ipv6 && !addr.Is4In6() {
			return addr.String()
		}
	}
	return ""
}

func inboundShareAddress(row *model.NodeInbound, node *model.Node) string {
	switch inboundShareMode(row) {
	case "custom":
		return strings.Trim(strings.TrimSpace(row.ShareHost), "[]")
	case "ipv4", "dual":
		return nodeShareIP(node, false)
	case "ipv6":
		return nodeShareIP(node, true)
	default:
		if node == nil {
			return ""
		}
		return firstNonEmpty(strings.Trim(strings.TrimSpace(node.ServerIP), "[]"), nodeShareIP(node, false), nodeShareIP(node, true))
	}
}

func inboundDirectShareEntries(row *model.NodeInbound, node *model.Node) []inboundShareEntrySpec {
	if inboundShareMode(row) == "dual" {
		var out []inboundShareEntrySpec
		if v4 := nodeShareIP(node, false); v4 != "" {
			out = append(out, inboundShareEntrySpec{Name: row.Name, Host: v4})
		}
		if v6 := nodeShareIP(node, true); v6 != "" {
			out = append(out, inboundShareEntrySpec{Name: row.Name + dualStackV6Suffix, Host: v6})
		}
		return out
	}
	host := inboundShareAddress(row, node)
	if host == "" {
		return nil
	}
	return []inboundShareEntrySpec{{Name: row.Name, Host: host}}
}

func validateShareAddress(mode, host string, node *model.Node) error {
	switch mode {
	case "auto":
		return nil
	case "custom":
		if strings.TrimSpace(host) == "" {
			return errText("请填写自定义分享地址")
		}
	case "ipv4", "ipv6":
		if nodeShareIP(node, mode == "ipv6") == "" {
			return errText("服务器未设置可用的 " + map[string]string{"ipv4": "IPv4", "ipv6": "IPv6"}[mode] + " 地址")
		}
	case "dual":
		if nodeShareIP(node, false) == "" || nodeShareIP(node, true) == "" {
			return errText("双栈分享需要服务器同时设置 IPv4 和 IPv6 地址")
		}
	default:
		return errText("分享地址类型无效")
	}
	return nil
}
