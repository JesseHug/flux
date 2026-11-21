package kernel

import (
	"context"
	"encoding/json"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

const statsCollectInterval = 15 * time.Second
const statsQueryTimeout = 3 * time.Second

// StatsQuerier queries and resets per-user traffic deltas from a running
// kernel's local stats endpoint (injected only by the full agent).
type StatsQuerier func(ctx context.Context, addr string) ([]UserCounter, error)

type userCounterKey struct {
	id  int64
	dir string
}

// SetStatsQuerier configures the local kernel stats collector. Leaving it
// unset keeps Lite agents free of gRPC dependencies.
func (m *Manager) SetStatsQuerier(fn StatsQuerier) {
	m.mu.Lock()
	m.statsQuerier = fn
	m.mu.Unlock()
}

func (m *Manager) hasStatsQuerier() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statsQuerier != nil
}

// ParseXrayUserStat parses an Xray StatsService metric name such as
// "user>>>u7@flux>>>traffic>>>uplink" or "user>>>u7>>>traffic>>>downlink".
func ParseXrayUserStat(name string) (id int64, dir string, ok bool) {
	parts := strings.Split(name, ">>>")
	if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
		return 0, "", false
	}
	switch parts[3] {
	case "uplink":
		dir = "up"
	case "downlink":
		dir = "down"
	default:
		return 0, "", false
	}
	user := strings.TrimSuffix(parts[1], "@flux")
	if !strings.HasPrefix(user, "u") {
		return 0, "", false
	}
	parsed, err := strconv.ParseInt(user[1:], 10, 64)
	if err != nil || parsed <= 0 {
		return 0, "", false
	}
	return parsed, dir, true
}

func (m *Manager) startStatsCollector() {
	m.mu.Lock()
	if m.IsLiteMode() || m.statsQuerier == nil || m.statsStarted {
		m.mu.Unlock()
		return
	}
	m.statsStarted = true
	stopCh := m.statsStop
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(statsCollectInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				m.drainAllStats()
			}
		}
	}()
}

func (m *Manager) drainAllStats() {
	m.mu.Lock()
	fn := m.statsQuerier
	lite := m.liteMode
	var addrs []string
	for _, inst := range m.instances {
		if inst.statsAddr != "" && len(inst.procs) > 0 {
			addrs = append(addrs, inst.statsAddr)
		}
	}
	m.mu.Unlock()
	if lite || fn == nil {
		return
	}
	for _, addr := range addrs {
		m.drainStatsAddr(fn, addr)
	}
}

func (m *Manager) drainInstanceStats(inst *instance) {
	m.mu.Lock()
	fn := m.statsQuerier
	lite := m.liteMode
	addr := inst.statsAddr
	running := len(inst.procs) > 0
	m.mu.Unlock()
	if lite || fn == nil || addr == "" || !running {
		return
	}
	m.drainStatsAddr(fn, addr)
}

func (m *Manager) drainStatsAddr(fn StatsQuerier, addr string) {
	ctx, cancel := context.WithTimeout(context.Background(), statsQueryTimeout)
	defer cancel()
	deltas, err := fn(ctx, addr)
	if err != nil || len(deltas) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range deltas {
		if d.ID <= 0 || d.Bytes <= 0 || (d.Dir != "up" && d.Dir != "down") {
			continue
		}
		m.statsTotals[userCounterKey{id: d.ID, dir: d.dirKey()}] += d.Bytes
	}
}

func (c UserCounter) dirKey() string { return c.Dir }

func (m *Manager) snapshotStatsUsers() (string, []UserCounter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	epoch := m.statsEpoch
	if len(m.statsTotals) == 0 {
		return epoch, nil
	}
	out := make([]UserCounter, 0, len(m.statsTotals))
	for k, v := range m.statsTotals {
		if v > 0 {
			out = append(out, UserCounter{ID: k.id, Dir: k.dir, Bytes: v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ID != out[j].ID {
			return out[i].ID < out[j].ID
		}
		return out[i].Dir < out[j].Dir
	})
	return epoch, out
}

// resolveStatsAddr verifies the requested loopback stats address can be bound
// (falling back to an ephemeral loopback port on conflict) and updates the
// rendered flux-api inbound port if it changed.
func resolveStatsAddr(req ApplyRequest, files []File) (string, []File) {
	if req.StatsAddr == "" || req.Type != "xray" {
		return req.StatsAddr, files
	}
	ln, err := net.Listen("tcp", req.StatsAddr)
	if err == nil {
		_ = ln.Close()
		return req.StatsAddr, files
	}
	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return req.StatsAddr, files
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return req.StatsAddr, files
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return req.StatsAddr, files
	}
	updated := make([]File, len(files))
	copy(updated, files)
	for i, f := range updated {
		if f.Name == "config.json" {
			updated[i].Content = rewriteXrayStatsPort(f.Content, port)
			break
		}
	}
	return addr, updated
}

func rewriteXrayStatsPort(content string, port int) string {
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return content
	}
	inbounds, ok := cfg["inbounds"].([]interface{})
	if !ok {
		return content
	}
	changed := false
	for _, raw := range inbounds {
		in, ok := raw.(map[string]interface{})
		if !ok || in["tag"] != "flux-api" {
			continue
		}
		in["port"] = port
		changed = true
		break
	}
	if !changed {
		return content
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return content
	}
	return string(b) + "\n"
}
