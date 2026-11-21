package config

import "testing"

func TestIdentityEnvPrefersNewNameIncludingEmptyValue(t *testing.T) {
	t.Setenv("FLVX_WEBAUTHN_ORIGIN", "https://oldest.example")
	if got := IdentityEnv("FLUX_WEBAUTHN_ORIGIN", "FLYX_WEBAUTHN_ORIGIN", "FLVX_WEBAUTHN_ORIGIN"); got != "https://oldest.example" {
		t.Fatalf("oldest fallback = %q", got)
	}
	t.Setenv("FLYX_WEBAUTHN_ORIGIN", "https://mid.example")
	if got := IdentityEnv("FLUX_WEBAUTHN_ORIGIN", "FLYX_WEBAUTHN_ORIGIN", "FLVX_WEBAUTHN_ORIGIN"); got != "https://mid.example" {
		t.Fatalf("legacy fallback = %q", got)
	}
	t.Setenv("FLUX_WEBAUTHN_ORIGIN", "https://new.example")
	if got := IdentityEnv("FLUX_WEBAUTHN_ORIGIN", "FLYX_WEBAUTHN_ORIGIN", "FLVX_WEBAUTHN_ORIGIN"); got != "https://new.example" {
		t.Fatalf("new value = %q", got)
	}
	t.Setenv("FLUX_WEBAUTHN_ORIGIN", "")
	if got := IdentityEnv("FLUX_WEBAUTHN_ORIGIN", "FLYX_WEBAUTHN_ORIGIN", "FLVX_WEBAUTHN_ORIGIN"); got != "" {
		t.Fatalf("explicit empty value must override legacy configuration, got %q", got)
	}
}
