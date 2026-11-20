//go:build linux

package kernel

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	lowMemThresholdBytes = 512 << 20
	minGoMemLimitMiB     = 24
	maxGoMemLimitMiB     = 160
)

var (
	cgroupV2MaxPath   = "/sys/fs/cgroup/memory.max"
	cgroupV1LimitPath = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
	procMemInfoPath   = "/proc/meminfo"
)

// detectedMemoryLimitBytes returns the effective memory limit of this machine or
// container in bytes, returning 0 if it cannot be determined.
func detectedMemoryLimitBytes() int64 {
	return readMemoryLimitBytes(cgroupV2MaxPath, cgroupV1LimitPath, procMemInfoPath)
}

func readMemoryLimitBytes(v2Path, v1Path, memInfoPath string) int64 {
	var cgroupLimit int64
	if b, err := os.ReadFile(v2Path); err == nil {
		s := strings.TrimSpace(string(b))
		if s != "" && s != "max" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
				cgroupLimit = v
			}
		}
	}
	if cgroupLimit == 0 {
		if b, err := os.ReadFile(v1Path); err == nil {
			s := strings.TrimSpace(string(b))
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 && v < (1<<50) {
				cgroupLimit = v
			}
		}
	}
	memTotal := readMemTotalBytes(memInfoPath)
	switch {
	case cgroupLimit > 0 && memTotal > 0:
		if cgroupLimit < memTotal {
			return cgroupLimit
		}
		return memTotal
	case cgroupLimit > 0:
		return cgroupLimit
	default:
		return memTotal
	}
}

func readMemTotalBytes(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil && kb > 0 {
				return kb * 1024
			}
		}
	}
	return 0
}

// kernelProcessEnv returns the child process environment with a conservative
// GOMEMLIMIT and GOGC on memory-constrained hosts (<= 512 MiB) for Go kernels.
func kernelProcessEnv(kernelType string) []string {
	return buildKernelProcessEnv(kernelType, detectedMemoryLimitBytes(), os.Environ())
}

func buildKernelProcessEnv(kernelType string, totalBytes int64, baseEnv []string) []string {
	if kernelType != "singbox" && kernelType != "xray" {
		return nil
	}
	if totalBytes <= 0 || totalBytes > lowMemThresholdBytes {
		return nil
	}
	totalMiB := totalBytes >> 20
	limitMiB := totalMiB * 32 / 100
	if totalMiB > 48 && limitMiB > totalMiB-24 {
		limitMiB = totalMiB - 24
	}
	if limitMiB < minGoMemLimitMiB {
		limitMiB = minGoMemLimitMiB
	}
	if limitMiB > maxGoMemLimitMiB {
		limitMiB = maxGoMemLimitMiB
	}
	return mergeEnv(baseEnv, map[string]string{
		"GOMEMLIMIT": fmt.Sprintf("%dMiB", limitMiB),
		"GOGC":       "50",
		"GODEBUG":    "disablethp=1",
	})
}

func mergeEnv(base []string, overrides map[string]string) []string {
	out := make([]string, 0, len(base)+len(overrides))
	present := make(map[string]bool, len(overrides))
	for _, kv := range base {
		k, _, ok := strings.Cut(kv, "=")
		if ok {
			if _, want := overrides[k]; want {
				present[k] = true
			}
		}
		out = append(out, kv)
	}
	for k, v := range overrides {
		if !present[k] {
			out = append(out, k+"="+v)
		}
	}
	return out
}
