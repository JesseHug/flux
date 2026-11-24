package kernel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestParseCFEntriesFormatsAndFilters(t *testing.T) {
	t.Run("txt formats and dedup", func(t *testing.T) {
		raw := `
# comment line
// another comment
104.16.132.229:443#移动优选
104.16.132.229#duplicate-same-default-port
172.67.1.2
[2606:4700::1]:8443#IPv6节点
2606:4700::2#BareIPv6
cf.877774.xyz:2053#优选域名
162.159.19.1 2083 空格分隔
http://invalid.example.com/path
not_a_host
`
		got := ParseCFEntries(raw, 443, "all", 20)
		if len(got) != 6 {
			t.Fatalf("expected 6 entries, got %d: %+v", len(got), got)
		}
		if got[0].Host != "104.16.132.229" || got[0].Port != 443 || got[0].Remark != "移动优选" {
			t.Fatalf("unexpected entry 0: %+v", got[0])
		}
		if got[1].Host != "172.67.1.2" || got[1].Port != 443 {
			t.Fatalf("unexpected entry 1: %+v", got[1])
		}
		if got[2].Host != "2606:4700::1" || got[2].Port != 8443 || got[2].Remark != "IPv6节点" {
			t.Fatalf("unexpected entry 2: %+v", got[2])
		}
		if got[3].Host != "2606:4700::2" || got[3].Port != 443 || got[3].Remark != "BareIPv6" {
			t.Fatalf("unexpected entry 3: %+v", got[3])
		}
		if got[4].Host != "cf.877774.xyz" || got[4].Port != 2053 || got[4].Remark != "优选域名" {
			t.Fatalf("unexpected entry 4: %+v", got[4])
		}
		if got[5].Host != "162.159.19.1" || got[5].Port != 2083 || got[5].Remark != "空格分隔" {
			t.Fatalf("unexpected entry 5: %+v", got[5])
		}
	})

	t.Run("csv cloudflare speed test", func(t *testing.T) {
		csvRaw := "IP 地址,已发送,已接收,丢包率,平均延迟,下载速度(MB/s),地区码\n104.16.1.1,4,4,0.00,45.2ms,12.5MB/s,SJC\n104.16.1.2,4,4,0.00,52.0ms,9.8MB/s,LAX\n"
		got := ParseCFEntries(csvRaw, 443, "ipv4", 1)
		if len(got) != 1 || got[0].Host != "104.16.1.1" || got[0].Port != 443 {
			t.Fatalf("unexpected csv parse: %+v", got)
		}
	})

	t.Run("json nested wetest style and filters", func(t *testing.T) {
		jsonRaw := `{
			"status": true,
			"info": {
				"CM": [{"ip": "104.18.1.1", "port": "443", "colo": "HKG"}],
				"CT": [{"ip": "2606:4700:4700::1111", "port": 2053, "line": "电信v6"}],
				"CNAME": [{"domain": "cf.example.org", "port": 8443, "name": "优选域名"}]
			}
		}`
		v4 := ParseCFEntries(jsonRaw, 443, "ipv4", 10)
		if len(v4) != 1 || v4[0].Host != "104.18.1.1" || v4[0].Remark != "HKG" {
			t.Fatalf("unexpected ipv4 filter result: %+v", v4)
		}
		v6 := ParseCFEntries(jsonRaw, 443, "ipv6", 10)
		if len(v6) != 1 || v6[0].Host != "2606:4700:4700::1111" || v6[0].Port != 2053 {
			t.Fatalf("unexpected ipv6 filter result: %+v", v6)
		}
		dom := ParseCFEntries(jsonRaw, 443, "domain", 10)
		if len(dom) != 1 || dom[0].Host != "cf.example.org" || dom[0].Port != 8443 {
			t.Fatalf("unexpected domain filter result: %+v", dom)
		}
	})
}

func TestCertModePEMValidation(t *testing.T) {
	certPEM, keyPEM, err := SelfSignedCert("cdn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	in := Inbound{
		Protocol:  ProtocolVLESS,
		Listen:    "::",
		Port:      2053,
		Transport: Transport{Type: TransportWS, Path: "/ws"},
		Security: Security{
			Type:     SecurityTLS,
			SNI:      "cdn.example.com",
			CertMode: CertModePEM,
			CertPEM:  certPEM,
			KeyPEM:   keyPEM,
		},
	}
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatalf("valid PEM failed: %v", err)
	}

	// Mismatched key must fail.
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherDER, err := x509.MarshalECPrivateKey(otherKey)
	if err != nil {
		t.Fatal(err)
	}
	badKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: otherDER}))
	bad := in
	bad.Security.KeyPEM = badKeyPEM
	if err := Normalize(TypeXray, &bad); err == nil {
		t.Fatal("mismatched PEM key must fail")
	}
}

func TestApplyCDNToEndpointBehavior(t *testing.T) {
	certPEM, keyPEM, err := SelfSignedCert("cdn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	base := Endpoint{
		Name:   "DR-CF",
		Host:   "23.95.86.111",
		Port:   57501,
		Direct: true,
		Inbound: Inbound{
			ID:        1,
			Protocol:  ProtocolVLESS,
			Port:      57501,
			Settings:  Settings{UUID: NewUUID()},
			Transport: Transport{Type: TransportWS, Path: "/ws"},
			Security: Security{
				Type:     SecurityTLS,
				SNI:      "cdn.example.com",
				CertMode: CertModeSelf,
				CertPEM:  certPEM,
				KeyPEM:   keyPEM,
			},
		},
	}
	cfg := CDNConfig{
		Enabled:  true,
		Domain:   "cdn.example.com",
		EdgePort: 443,
	}
	if err := NormalizeCDNConfig(base.Inbound, &cfg); err != nil {
		t.Fatalf("NormalizeCDNConfig: %v", err)
	}

	ep := ApplyCDNToEndpoint(base, cfg, "104.16.132.229", 443, "DR-CF · 移动")
	if ep.Host != "104.16.132.229" || ep.Port != 443 {
		t.Fatalf("unexpected endpoint host/port: %+v", ep)
	}
	if ep.Inbound.Transport.Host != "cdn.example.com" || ep.Inbound.Security.SNI != "cdn.example.com" {
		t.Fatalf("Host/SNI must be CDN domain: %+v / %+v", ep.Inbound.Transport, ep.Inbound.Security)
	}
	if !ep.TrustedEdgeTLS || ep.selfSigned() {
		t.Fatalf("CF edge endpoint must not set selfSigned/allowInsecure")
	}

	uri, ok := ShareURI(ep)
	if !ok || strings.Contains(uri, "allowInsecure") || !strings.Contains(uri, "host=cdn.example.com") || !strings.Contains(uri, "sni=cdn.example.com") {
		t.Fatalf("unexpected ShareURI: ok=%v uri=%s", ok, uri)
	}
	clash, ok := ClashProxy(ep)
	if !ok {
		t.Fatal("ClashProxy failed")
	}
	clashStr := marshalYAML(yamlNode{{"proxies", []interface{}{clash}}})
	if strings.Contains(clashStr, "skip-cert-verify") || !strings.Contains(clashStr, `Host: "cdn.example.com"`) || !strings.Contains(clashStr, `servername: "cdn.example.com"`) {
		t.Fatalf("unexpected Clash YAML:\n%s", clashStr)
	}
	sb, ok := SingboxOutbounds(ep)
	if !ok || len(sb) != 1 {
		t.Fatalf("SingboxOutbounds failed")
	}
	tlsObj, _ := sb[0]["tls"].(map[string]interface{})
	if _, hasInsecure := tlsObj["insecure"]; hasInsecure {
		t.Fatalf("sing-box outbound must not set tls.insecure for CF edge: %+v", tlsObj)
	}

	// Plain origin (Flexible) upgraded to edge TLS on 443.
	flexBase := base
	flexBase.Inbound.Security = Security{Type: SecurityNone}
	flexEp := ApplyCDNToEndpoint(flexBase, cfg, "104.16.132.229", 443, "DR-Flex")
	if flexEp.Inbound.Security.Type != SecurityTLS || flexEp.Inbound.Security.SNI != "cdn.example.com" || !flexEp.TrustedEdgeTLS {
		t.Fatalf("expected Flexible origin to upgrade to edge TLS: %+v", flexEp.Inbound.Security)
	}

	// Edge HTTP port 80 disables client TLS.
	tlsFalse := false
	httpCfg := CDNConfig{Enabled: true, Domain: "cdn.example.com", EdgePort: 80, EdgeTLS: &tlsFalse}
	httpEp := ApplyCDNToEndpoint(base, httpCfg, "104.16.132.229", 80, "DR-HTTP")
	if httpEp.Inbound.Security.Type != SecurityNone || httpEp.TrustedEdgeTLS {
		t.Fatalf("expected HTTP edge port to disable client TLS: %+v", httpEp.Inbound.Security)
	}
}
