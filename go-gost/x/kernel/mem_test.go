//go:build linux

package kernel

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReadMemoryLimitBytesAndProcessEnv(t *testing.T) {
	dir := t.TempDir()
	v2 := filepath.Join(dir, "memory.max")
	v1 := filepath.Join(dir, "memory.limit_in_bytes")
	meminfo := filepath.Join(dir, "meminfo")

	if err := os.WriteFile(meminfo, []byte("MemTotal:        1048576 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// cgroup v2 caps a 1 GiB host down to 128 MB (122 MiB).
	if err := os.WriteFile(v2, []byte("128000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readMemoryLimitBytes(v2, v1, meminfo)
	if got != 128000000 {
		t.Fatalf("readMemoryLimitBytes = %d, want 128000000", got)
	}

	env := buildKernelProcessEnv("singbox", got, []string{"PATH=/usr/bin"})
	if !slices.Contains(env, "GOMEMLIMIT=39MiB") || !slices.Contains(env, "GOGC=50") {
		t.Fatalf("unexpected env for 122 MiB singbox: %v", env)
	}

	// Non-Go kernels (realm/snell) and large hosts (> 512 MiB) leave env untouched.
	if env := buildKernelProcessEnv("realm", got, []string{"PATH=/usr/bin"}); env != nil {
		t.Fatalf("expected nil env for realm, got %v", env)
	}
	if env := buildKernelProcessEnv("singbox", 1024<<20, []string{"PATH=/usr/bin"}); env != nil {
		t.Fatalf("expected nil env for 1 GiB host, got %v", env)
	}

	// cgroup v2 "max" falls back to /proc/meminfo, and existing GOMEMLIMIT is preserved.
	if err := os.WriteFile(v2, []byte("max\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(meminfo, []byte("MemTotal:         262144 kB\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = readMemoryLimitBytes(v2, v1, meminfo)
	if got != 256<<20 {
		t.Fatalf("readMemoryLimitBytes fallback = %d, want %d", got, 256<<20)
	}
	env = buildKernelProcessEnv("xray", got, []string{"GOMEMLIMIT=44MiB"})
	if !slices.Contains(env, "GOMEMLIMIT=44MiB") || slices.Contains(env, "GOMEMLIMIT=81MiB") {
		t.Fatalf("existing GOMEMLIMIT should be preserved: %v", env)
	}
}
