package kernel

import (
	"encoding/json"
	"testing"
)

func TestRealityNoStealNormalize(t *testing.T) {
	in := Inbound{
		Protocol:  ProtocolVLESS,
		Port:      443,
		Transport: Transport{Type: TransportTCP},
		Security: Security{
			Type:           SecurityReality,
			SNI:            "dl-cdn.alpinelinux.org",
			RealityNoSteal: true,
		},
	}
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if !in.Security.RealityNoSteal {
		t.Fatal("expected RealityNoSteal to be true")
	}
	if in.Security.RealityFallbackPort < 1024 || in.Security.RealityFallbackPort > 65535 {
		t.Fatalf("unexpected fallback port: %d", in.Security.RealityFallbackPort)
	}
	if len(in.Security.RealityServerNames) != 1 || in.Security.RealityServerNames[0] != "dl-cdn.alpinelinux.org" {
		t.Fatalf("expected RealityServerNames to default to SNI, got: %v", in.Security.RealityServerNames)
	}

	// When disabled, auxiliary fields must be wiped
	in.Security.RealityNoSteal = false
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if in.Security.RealityFallbackPort != 0 || in.Security.RealityServerNames != nil {
		t.Fatalf("auxiliary fields not cleared when RealityNoSteal is false: %+v", in.Security)
	}
}

func TestRealityNoStealXray(t *testing.T) {
	in := Inbound{
		ID:        1,
		Protocol:  ProtocolVLESS,
		Port:      443,
		Settings:  Settings{UUID: testUUID, Flow: FlowVision},
		Transport: Transport{Type: TransportTCP},
		Security: Security{
			Type:                SecurityReality,
			SNI:                 "dl-cdn.alpinelinux.org",
			RealityDest:         "dl-cdn.alpinelinux.org:443",
			RealityPrivateKey:   testRealityKey,
			RealityShortIDs:     []string{"0123456789abcdef"},
			RealityNoSteal:      true,
			RealityFallbackPort: 28443,
			RealityServerNames:  []string{"dl-cdn.alpinelinux.org", "alpinelinux.org"},
		},
	}
	if err := Normalize(TypeXray, &in); err != nil {
		t.Fatalf("normalize: %v", err)
	}

	out, err := Render(TypeXray, []Inbound{in})
	if err != nil {
		t.Fatalf("render xray: %v", err)
	}

	content := out.Files[0].Content
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	inbounds := raw["inbounds"].([]interface{})
	if len(inbounds) != 2 {
		t.Fatalf("expected 2 inbounds (main + fallback), got %d", len(inbounds))
	}

	// Verify main inbound dest points to local loopback
	main := inbounds[0].(map[string]interface{})
	stream := main["streamSettings"].(map[string]interface{})
	reality := stream["realitySettings"].(map[string]interface{})
	if reality["dest"] != "127.0.0.1:28443" {
		t.Fatalf("expected reality dest 127.0.0.1:28443, got %v", reality["dest"])
	}

	// Verify fallback companion inbound
	fb := inbounds[1].(map[string]interface{})
	if fb["tag"] != "in-1-fallback" || fb["protocol"] != "dokodemo-door" || int(fb["port"].(float64)) != 28443 {
		t.Fatalf("unexpected fallback inbound: %+v", fb)
	}
	sniffing := fb["sniffing"].(map[string]interface{})
	if sniffing["enabled"] != true || sniffing["routeOnly"] != true {
		t.Fatalf("unexpected sniffing: %+v", sniffing)
	}

	// Verify routing rules
	routing := raw["routing"].(map[string]interface{})
	rules := routing["rules"].([]interface{})
	if len(rules) < 2 {
		t.Fatalf("expected at least 2 routing rules, got %d", len(rules))
	}
	r1 := rules[0].(map[string]interface{})
	if r1["outboundTag"] != "direct" {
		t.Fatalf("expected first rule outboundTag direct, got %v", r1["outboundTag"])
	}
	r2 := rules[1].(map[string]interface{})
	if r2["outboundTag"] != "block" {
		t.Fatalf("expected second rule outboundTag block, got %v", r2["outboundTag"])
	}
}

func TestRealityNoStealSingbox(t *testing.T) {
	in := Inbound{
		ID:        1,
		Protocol:  ProtocolVLESS,
		Port:      443,
		Settings:  Settings{UUID: testUUID, Flow: FlowVision},
		Transport: Transport{Type: TransportTCP},
		Security: Security{
			Type:                SecurityReality,
			SNI:                 "dl-cdn.alpinelinux.org",
			RealityDest:         "dl-cdn.alpinelinux.org:443",
			RealityPrivateKey:   testRealityKey,
			RealityShortIDs:     []string{"0123456789abcdef"},
			RealityNoSteal:      true,
			RealityFallbackPort: 28443,
			RealityServerNames:  []string{"dl-cdn.alpinelinux.org"},
		},
	}
	if err := Normalize(TypeSingbox, &in); err != nil {
		t.Fatalf("normalize: %v", err)
	}

	out, err := Render(TypeSingbox, []Inbound{in})
	if err != nil {
		t.Fatalf("render singbox: %v", err)
	}

	content := out.Files[0].Content
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	inbounds := raw["inbounds"].([]interface{})
	if len(inbounds) != 2 {
		t.Fatalf("expected 2 inbounds (main + fallback), got %d", len(inbounds))
	}

	// Verify main handshake points to local loopback
	main := inbounds[0].(map[string]interface{})
	tls := main["tls"].(map[string]interface{})
	reality := tls["reality"].(map[string]interface{})
	hs := reality["handshake"].(map[string]interface{})
	if hs["server"] != "127.0.0.1" || int(hs["server_port"].(float64)) != 28443 {
		t.Fatalf("unexpected reality handshake: %+v", hs)
	}

	// Verify fallback direct inbound
	fb := inbounds[1].(map[string]interface{})
	if fb["tag"] != "in-1-fallback" || fb["type"] != "direct" || int(fb["listen_port"].(float64)) != 28443 {
		t.Fatalf("unexpected fallback inbound: %+v", fb)
	}
	if fb["override_address"] != "dl-cdn.alpinelinux.org" || int(fb["override_port"].(float64)) != 443 {
		t.Fatalf("unexpected override address/port: %+v", fb)
	}

	// Verify outbounds contain block
	outbounds := raw["outbounds"].([]interface{})
	hasBlock := false
	for _, o := range outbounds {
		ob := o.(map[string]interface{})
		if ob["tag"] == "block" && ob["type"] == "block" {
			hasBlock = true
			break
		}
	}
	if !hasBlock {
		t.Fatalf("expected outbounds to contain block, got: %+v", outbounds)
	}

	// Verify route rules
	route := raw["route"].(map[string]interface{})
	rules := route["rules"].([]interface{})
	if len(rules) < 3 {
		t.Fatalf("expected at least 3 route rules, got %d", len(rules))
	}
	rSniff := rules[0].(map[string]interface{})
	if rSniff["action"] != "sniff" {
		t.Fatalf("expected rule 0 action sniff, got %v", rSniff["action"])
	}
	rDirect := rules[1].(map[string]interface{})
	if rDirect["outbound"] != "direct" {
		t.Fatalf("expected rule 1 outbound direct, got %v", rDirect["outbound"])
	}
	rBlock := rules[2].(map[string]interface{})
	if rBlock["outbound"] != "block" {
		t.Fatalf("expected rule 2 outbound block, got %v", rBlock["outbound"])
	}
}
