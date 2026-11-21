// Package identity holds the agent's installation names and legacy aliases.
package identity

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	AgentName     = "flux-agent"
	LiteAgentName = "flux-agent-lite"
)

// IsKnownAgentName reports whether base matches a current or legacy agent
// executable/service name handled in-place by upgrades and uninstallers.
func IsKnownAgentName(base string) bool {
	switch base {
	case AgentName, LiteAgentName, "flyx-agent", "flyx-agent-lite", "flux_agent":
		return true
	default:
		return false
	}
}

func Env(names ...string) string {
	for _, name := range names {
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
	}
	return ""
}

// Old agents install an upgrade at their existing binary path. Following the
// running executable preserves its service, kernels, spool and rollback file.
func installation(executable string) (dir, binary, service string) {
	base := filepath.Base(executable)
	if IsKnownAgentName(base) {
		return filepath.Dir(executable), executable, base
	}
	return "/etc/flux-agent", "/etc/flux-agent/flux-agent", AgentName
}

func DataDir() string {
	if dir := strings.TrimSpace(Env("FLUX_AGENT_DATA_DIR", "FLYX_AGENT_DATA_DIR")); dir != "" {
		return dir
	}
	executable, _ := os.Executable()
	dir, _, _ := installation(executable)
	return dir
}

func BinaryPath() string {
	executable, _ := os.Executable()
	_, binary, _ := installation(executable)
	return binary
}
