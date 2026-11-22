package kernel

import (
	"testing"
)

func TestSingboxNTPOptions(t *testing.T) {
	in := Inbound{
		ID:       1,
		Protocol: ProtocolShadowsocks,
		Listen:   "::",
		Port:     18388,
		Settings: Settings{Method: DefaultSSMethod},
		Users:    chainUser,
	}
	if err := Normalize(TypeSingbox, &in); err != nil {
		t.Fatal(err)
	}

	withoutNTP, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{SingboxNTP: false})
	if err != nil {
		t.Fatal(err)
	}
	oldCfg := renderJSONString(t, withoutNTP.Files[0].Content)
	if oldCfg["ntp"] != nil {
		t.Fatalf("expected ntp to be omitted when disabled, got %v", oldCfg["ntp"])
	}

	withNTP, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{SingboxNTP: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg := renderJSONString(t, withNTP.Files[0].Content)
	ntp, ok := cfg["ntp"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected ntp object, got %v", cfg["ntp"])
	}
	if ntp["enabled"] != true || ntp["server"] != "time.apple.com" || ntp["server_port"] != float64(123) || ntp["interval"] != "30m" {
		t.Fatalf("unexpected ntp block: %v", ntp)
	}
	if _, hasWrite := ntp["write_to_system"]; hasWrite {
		t.Fatalf("must not set write_to_system: %v", ntp)
	}
}

func TestSingboxCacheFileOption(t *testing.T) {
	in := Inbound{
		ID:       2,
		Protocol: ProtocolVLESS,
		Listen:   "::",
		Port:     8443,
		Settings: Settings{UUID: "00000000-0000-4000-8000-000000000000"},
		Users:    chainUser,
		Security: Security{Type: SecurityReality, SNI: "example.com", RealityDest: "example.com:443"},
		Split: []SplitRule{
			{Categories: []string{"openai"}, Action: SplitBlock},
		},
	}
	if err := Normalize(TypeSingbox, &in); err != nil {
		t.Fatal(err)
	}

	defaultOut, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defaultCfg := renderJSONString(t, defaultOut.Files[0].Content)
	exp, ok := defaultCfg["experimental"].(map[string]interface{})
	if !ok || exp["cache_file"] == nil {
		t.Fatalf("expected experimental.cache_file by default when .srs rules exist: %v", defaultCfg["experimental"])
	}

	noCacheOut, err := RenderWith(TypeSingbox, []Inbound{in}, RenderOptions{SingboxNoCacheFile: true})
	if err != nil {
		t.Fatal(err)
	}
	noCacheCfg := renderJSONString(t, noCacheOut.Files[0].Content)
	if expOff, ok := noCacheCfg["experimental"].(map[string]interface{}); ok && expOff["cache_file"] != nil {
		t.Fatalf("expected cache_file to be omitted when SingboxNoCacheFile=true, got %v", expOff["cache_file"])
	}
}
