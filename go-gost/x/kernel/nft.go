package kernel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const nftTable = "flux_kernel"
const legacyNftTable = "flvx_kernel"
const legacyFlyxNftTable = "flyx_kernel"

func isLegacyNftTable(name string) bool {
	return name == legacyFlyxNftTable || name == legacyNftTable
}

func isManagedNftTable(name string) bool {
	return name == nftTable || isLegacyNftTable(name)
}

// nftRunner executes `nft` with a script on stdin (or arguments). It is a
// variable so tests can capture scripts without root.
var nftRunner = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "nft", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("nft: %s", msg)
	}
	return out, nil
}

// errNftDenied: nft is installed but the agent may not use it, as in
// unprivileged LXC/OpenVZ containers. Permissions do not change while the
// agent runs, so this is probed once.
var errNftDenied = errors.New("本机没有 nftables 权限（常见于 LXC、OpenVZ 等容器），无法使用流量统计、入站白名单、端口跳跃和限速")

var nftProbe struct {
	sync.Mutex
	denied bool
	ok     bool
}

var nftAvailable = func() error {
	if _, err := exec.LookPath("nft"); err != nil {
		return err
	}
	return probeNftPermission(nftRunner)
}

// probeNftPermission lists the tables once to tell a usable nft from one the
// kernel refuses. Only a definite answer is cached; a timeout is retried.
func probeNftPermission(run func(ctx context.Context, stdin string, args ...string) ([]byte, error)) error {
	nftProbe.Lock()
	defer nftProbe.Unlock()
	switch {
	case nftProbe.denied:
		return errNftDenied
	case nftProbe.ok:
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := run(ctx, "", "list", "tables")
	if err == nil {
		nftProbe.ok = true
		return nil
	}
	if nftPermissionError(err) {
		nftProbe.denied = true
		return errNftDenied
	}
	return nil // some other failure: let the real call report it
}

func nftPermissionError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"operation not permitted", "permission denied", "you must be root"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// nftUnavailableText explains why nftables cannot be used.
func nftUnavailableText(err error) string {
	if errors.Is(err, errNftDenied) {
		return errNftDenied.Error()
	}
	return "未安装 nftables，无法统计流量"
}

type counterRule struct {
	InboundID int64
	Port      int
	Proto     string
}

func (c counterRule) name(dir string) string {
	return fmt.Sprintf("%s_%d_%s", dir, c.InboundID, c.Proto)
}

// expandPorts turns inbound ports into one rule per L4 protocol, sorted so
// the generated ruleset (and its signature) is stable.
func expandPorts(ports []Port) []counterRule {
	seen := make(map[counterRule]struct{})
	var rules []counterRule
	for _, p := range ports {
		for _, proto := range strings.Split(p.Network, ",") {
			proto = strings.TrimSpace(proto)
			if proto != "tcp" && proto != "udp" {
				continue
			}
			r := counterRule{InboundID: p.InboundID, Port: p.Port, Proto: proto}
			if _, ok := seen[r]; ok {
				continue
			}
			seen[r] = struct{}{}
			rules = append(rules, r)
		}
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].InboundID != rules[j].InboundID {
			return rules[i].InboundID < rules[j].InboundID
		}
		return rules[i].Proto < rules[j].Proto
	})
	return rules
}

// Users' connections carry the socket mark userMarkBase|id (set by the
// panel-rendered kernel config). Packets out with it are the user's upload;
// the mark is copied to the connection so packets coming back count as the
// download.
const (
	userMarkBase = 0x46560000
	userMarkMax  = 0xffff
)

func userCounterName(dir string, id int64) string { return fmt.Sprintf("u%s_%d", dir, id) }

func sortedUsers(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && id <= userMarkMax && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// hopRule redirects a UDP port range to an inbound port (port hopping).
type hopRule struct {
	From, To, Port int
}

func expandHops(ports []Port) []hopRule {
	var hops []hopRule
	for _, p := range ports {
		if p.HopFrom > 0 && p.HopTo > p.HopFrom {
			hops = append(hops, hopRule{From: p.HopFrom, To: p.HopTo, Port: p.Port})
		}
	}
	sort.Slice(hops, func(i, j int) bool { return hops[i].From < hops[j].From })
	return hops
}

// allowGuard is the source whitelist of one inbound, split by family.
type allowGuard struct {
	v4, v6 []string
}

// expandGuards collects the whitelists keyed by inbound. Entries were
// validated as CIDRs; they are re-rendered from the parsed form so nothing
// but an address can reach the nft script.
func expandGuards(ports []Port) map[int64]allowGuard {
	guards := make(map[int64]allowGuard)
	for _, p := range ports {
		if p.Allow == nil {
			continue
		}
		g := guards[p.InboundID]
		for _, c := range p.Allow {
			_, ipnet, err := net.ParseCIDR(c)
			if err != nil {
				continue
			}
			if ipnet.IP.To4() != nil {
				g.v4 = append(g.v4, ipnet.String())
			} else {
				g.v6 = append(g.v6, ipnet.String())
			}
		}
		sort.Strings(g.v4)
		sort.Strings(g.v6)
		guards[p.InboundID] = g
	}
	return guards
}

// rateLimit is one inbound's speed limits in Mbit/s (0 = none).
type rateLimit struct {
	Up, Down int
}

// expandLimits collects the speed limits keyed by inbound.
func expandLimits(ports []Port) map[int64]rateLimit {
	limits := make(map[int64]rateLimit)
	for _, p := range ports {
		if p.LimitUpMbps <= 0 && p.LimitDownMbps <= 0 {
			continue
		}
		l := limits[p.InboundID]
		l.Up = max(l.Up, max(p.LimitUpMbps, 0))
		l.Down = max(l.Down, max(p.LimitDownMbps, 0))
		limits[p.InboundID] = l
	}
	return limits
}

// limitSpec renders a speed limit as an nft limit object body. The bucket
// holds about 0.2 s of traffic (at least 64 KiB): enough for TCP to ramp up
// against a policer on long-distance paths without letting bursts through
// for long.
func limitSpec(mbps int) string {
	rate := int64(mbps) * 125000 // bytes per second
	burst := max(rate/5, 65536)
	return fmt.Sprintf("rate over %d bytes/second burst %d bytes", rate, burst)
}

func writeSet(b *strings.Builder, name, typ string, elems []string) {
	fmt.Fprintf(b, "\tset %s {\n\t\ttype %s\n\t\tflags interval\n\t\tauto-merge\n", name, typ)
	if len(elems) > 0 {
		fmt.Fprintf(b, "\t\telements = { %s }\n", strings.Join(elems, ", "))
	}
	b.WriteString("\t}\n")
}

// nftScript builds the atomic ruleset. Chains have policy accept and only
// count, except that a port with a whitelist drops packets from sources not
// on it (only that port; nothing else on the host is affected).
//
// Hop ranges are redirected before filtering, so counters and whitelists see
// the inbound port. Only traffic addressed to this host is redirected; the
// output hook covers clients running on the node itself.
//
// Speed limits are named limit objects, one per direction and inbound, so the
// TCP and UDP rules of an inbound share one budget. Packets over it are
// dropped (policing; TCP backs off), before they are counted: upload in the
// input chain by destination port, download in the output chain by source
// port. Replies to port-hopped packets still carry the inbound port there
// (NAT is undone later, in postrouting), so hopping is limited too.
func nftScript(rules []counterRule, guards map[int64]allowGuard, hops []hopRule, users []int64, limits map[int64]rateLimit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "table inet %s {}\n", nftTable)
	fmt.Fprintf(&b, "delete table inet %s\n", nftTable)
	if len(rules) == 0 && len(users) == 0 {
		return b.String()
	}
	fmt.Fprintf(&b, "table inet %s {\n", nftTable)
	for _, r := range rules {
		fmt.Fprintf(&b, "\tcounter %s {}\n", r.name("in"))
		fmt.Fprintf(&b, "\tcounter %s {}\n", r.name("out"))
	}
	for _, id := range users {
		fmt.Fprintf(&b, "\tcounter %s {}\n", userCounterName("up", id))
		fmt.Fprintf(&b, "\tcounter %s {}\n", userCounterName("down", id))
	}
	limited := make([]int64, 0, len(limits))
	for id := range limits {
		limited = append(limited, id)
	}
	sort.Slice(limited, func(i, j int) bool { return limited[i] < limited[j] })
	for _, id := range limited {
		if l := limits[id]; l.Up > 0 {
			fmt.Fprintf(&b, "\tlimit rl_up_%d { %s }\n", id, limitSpec(l.Up))
		}
		if l := limits[id]; l.Down > 0 {
			fmt.Fprintf(&b, "\tlimit rl_down_%d { %s }\n", id, limitSpec(l.Down))
		}
	}
	guarded := make([]int64, 0, len(guards))
	for id := range guards {
		guarded = append(guarded, id)
	}
	sort.Slice(guarded, func(i, j int) bool { return guarded[i] < guarded[j] })
	for _, id := range guarded {
		writeSet(&b, fmt.Sprintf("allow4_%d", id), "ipv4_addr", guards[id].v4)
		writeSet(&b, fmt.Sprintf("allow6_%d", id), "ipv6_addr", guards[id].v6)
	}
	b.WriteString("\tchain input {\n\t\ttype filter hook input priority -150; policy accept;\n")
	for _, r := range rules {
		if _, ok := guards[r.InboundID]; !ok {
			continue
		}
		fmt.Fprintf(&b, "\t\tmeta nfproto ipv4 %s dport %d ip saddr != @allow4_%d drop\n", r.Proto, r.Port, r.InboundID)
		fmt.Fprintf(&b, "\t\tmeta nfproto ipv6 %s dport %d ip6 saddr != @allow6_%d drop\n", r.Proto, r.Port, r.InboundID)
	}
	for _, r := range rules {
		if limits[r.InboundID].Up > 0 {
			fmt.Fprintf(&b, "\t\t%s dport %d limit name \"rl_up_%d\" drop\n", r.Proto, r.Port, r.InboundID)
		}
	}
	for _, r := range rules {
		fmt.Fprintf(&b, "\t\t%s dport %d counter name %s\n", r.Proto, r.Port, r.name("in"))
	}
	for _, id := range users {
		fmt.Fprintf(&b, "\t\tct mark 0x%08x counter name %s\n", userMarkBase|id, userCounterName("down", id))
	}
	b.WriteString("\t}\n\tchain output {\n\t\ttype filter hook output priority -150; policy accept;\n")
	for _, r := range rules {
		if limits[r.InboundID].Down > 0 {
			fmt.Fprintf(&b, "\t\t%s sport %d limit name \"rl_down_%d\" drop\n", r.Proto, r.Port, r.InboundID)
		}
	}
	for _, r := range rules {
		fmt.Fprintf(&b, "\t\t%s sport %d counter name %s\n", r.Proto, r.Port, r.name("out"))
	}
	for _, id := range users {
		fmt.Fprintf(&b, "\t\tmeta mark 0x%08x ct mark set meta mark counter name %s\n", userMarkBase|id, userCounterName("up", id))
	}
	b.WriteString("\t}\n")
	if len(hops) > 0 {
		for _, hook := range []string{"prerouting", "output"} {
			fmt.Fprintf(&b, "\tchain hop_%s {\n\t\ttype nat hook %s priority -100; policy accept;\n", hook, hook)
			for _, h := range hops {
				fmt.Fprintf(&b, "\t\tfib daddr type local udp dport %d-%d redirect to :%d\n", h.From, h.To, h.Port)
			}
			b.WriteString("\t}\n")
		}
	}
	b.WriteString("}\n")
	return b.String()
}

type nftState struct {
	Table     string           `json:"table,omitempty"`
	Signature string           `json:"signature"`
	Epoch     string           `json:"epoch"`
	Offsets   map[string]int64 `json:"offsets,omitempty"`
}

// nftCounters owns the FLUX kernel table (or an existing legacy table). The epoch changes whenever the
// table is rebuilt so the panel knows the counters restarted from zero.
type nftCounters struct {
	mu        sync.Mutex
	statePath string
	state     nftState
	lastErr   string
}

func newNftCounters(baseDir string) *nftCounters {
	n := &nftCounters{statePath: filepath.Join(baseDir, "nft.json"), state: nftState{Table: nftTable}}
	if b, err := os.ReadFile(n.statePath); err == nil {
		// Missing table identifies a state written by a pre-migration agent.
		var state nftState
		if json.Unmarshal(b, &state) == nil {
			if state.Table == "" {
				state.Table = legacyNftTable
			}
			if isManagedNftTable(state.Table) {
				n.state = state
			}
		}
	}
	return n
}

func (n *nftCounters) hasState() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.state.Signature != ""
}

func (n *nftCounters) hasOffsets() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.state.Offsets) > 0
}

// Inspect live tables as well as disk state: nft.json can be lost while the
// rules remain installed. Never create a second ruleset beside the old one.
// Listings use nft's text output: older nft builds (1.0.6 in Debian bookworm
// images) can segfault printing JSON on newer kernels, and some lack JSON.
func (n *nftCounters) resolveTable(ctx context.Context) (string, bool, error) {
	out, err := nftRunner(ctx, "", "list", "tables")
	if err != nil {
		return "", false, err
	}
	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "table" || fields[1] != "inet" {
			continue
		}
		name := strings.Trim(fields[2], `"`)
		if isManagedNftTable(name) {
			found = append(found, name)
		}
	}
	if len(found) > 1 {
		return "", false, fmt.Errorf("同时发现新旧节点 nftables 表，请先确认要保留的安装")
	}
	table := n.state.Table
	if len(found) == 1 {
		table = found[0]
	} else if table == "" {
		table = nftTable
	}
	return table, len(found) > 0, nil
}

// removeTables deletes both the current and any legacy tables, whichever exist.
func (n *nftCounters) removeTables() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if nftAvailable() != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := nftRunner(ctx, "", "list", "tables")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "table" || fields[1] != "inet" {
			continue
		}
		name := strings.Trim(fields[2], `"`)
		if !isManagedNftTable(name) {
			continue
		}
		if _, err := nftRunner(ctx, "", "delete", "table", "inet", name); err != nil {
			return err
		}
	}
	n.state = nftState{Table: nftTable}
	_ = os.Remove(n.statePath)
	return nil
}

func (n *nftCounters) sync(ports []Port, users []int64) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := nftAvailable(); err != nil {
		n.lastErr = nftUnavailableText(err)
		if errors.Is(err, errNftDenied) {
			return err
		}
		return fmt.Errorf("未安装 nftables: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	table, exists, err := n.resolveTable(ctx)
	if err != nil {
		n.lastErr = err.Error()
		return err
	}
	rules := expandPorts(ports)
	users = sortedUsers(users)
	script := nftScript(rules, expandGuards(ports), expandHops(ports), users, expandLimits(ports))
	if isLegacyNftTable(table) {
		script = strings.ReplaceAll(script, nftTable, table)
	}
	sig := hashString(script)

	if sig == n.state.Signature && ((len(rules) == 0 && len(users) == 0) || exists) {
		n.lastErr = ""
		return nil
	}
	// Preserve accumulated counter values from the live table before replacing
	// it so config or user-list changes never drop unpolled traffic.
	epoch := newEpoch()
	var offsets map[string]int64
	if exists && n.state.Epoch != "" {
		if live, readErr := n.readTableValues(ctx, table); readErr == nil {
			epoch = n.state.Epoch
			offsets = mergeNftOffsets(n.state.Offsets, live)
		}
	}
	if _, err := nftRunner(ctx, script, "-f", "-"); err != nil {
		n.lastErr = err.Error()
		return err
	}
	n.lastErr = ""
	n.state = nftState{Table: table, Signature: sig, Epoch: epoch, Offsets: offsets}
	if b, err := json.Marshal(n.state); err == nil {
		_ = writeFileAtomic(n.statePath, b, 0o600)
	}
	return nil
}

func mergeNftOffsets(prev map[string]int64, live []nftCounterValue) map[string]int64 {
	out := make(map[string]int64, len(prev)+len(live))
	for name, val := range prev {
		if val > 0 {
			out[name] = val
		}
	}
	for _, c := range live {
		if c.bytes > 0 {
			out[c.name] += c.bytes
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (n *nftCounters) readTableValues(ctx context.Context, table string) ([]nftCounterValue, error) {
	out, err := nftRunner(ctx, "", "list", "counters", "table", "inet", table)
	if err != nil {
		return nil, err
	}
	return parseCounterListing(out)
}

// read returns the current counters together with the epoch they belong to.
func (n *nftCounters) read() ([]Counter, []UserCounter, string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err := nftAvailable(); err != nil {
		return nil, nil, "", errors.New(nftUnavailableText(err))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	values, err := n.readTableValues(ctx, n.state.Table)
	if err != nil {
		// When the table was intentionally removed (no active ports/users),
		// still emit carried offsets so final traffic is not lost.
		if len(n.state.Offsets) > 0 && n.state.Epoch != "" {
			if _, exists, rerr := n.resolveTable(ctx); rerr == nil && !exists {
				counters, users := parseCounterValues(offsetsToValues(n.state.Table, n.state.Offsets))
				return counters, users, n.state.Epoch, nil
			}
		}
		return nil, nil, "", err
	}
	combined := combineTableAndOffsets(n.state.Table, values, n.state.Offsets)
	counters, users := parseCounterValues(combined)
	return counters, users, n.state.Epoch, nil
}

func offsetsToValues(table string, offsets map[string]int64) []nftCounterValue {
	names := make([]string, 0, len(offsets))
	for name, val := range offsets {
		if val > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]nftCounterValue, 0, len(names))
	for _, name := range names {
		out = append(out, nftCounterValue{table: table, name: name, bytes: offsets[name]})
	}
	return out
}

func combineTableAndOffsets(table string, live []nftCounterValue, offsets map[string]int64) []nftCounterValue {
	if len(offsets) == 0 {
		return live
	}
	seen := make(map[string]bool, len(live))
	out := make([]nftCounterValue, 0, len(live)+len(offsets))
	for _, c := range live {
		if isManagedNftTable(c.table) {
			seen[c.name] = true
			c.bytes += offsets[c.name]
		}
		out = append(out, c)
	}
	var extra []string
	for name, val := range offsets {
		if val > 0 && !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		out = append(out, nftCounterValue{table: table, name: name, bytes: offsets[name]})
	}
	return out
}

type nftCounterValue struct {
	table, name string
	bytes       int64
}

// parseCounterListing reads `nft list counters` text:
//
//	table inet flux_kernel {
//		counter in_7_tcp {
//			packets 3 bytes 1200
//		}
//	}
func parseCounterListing(raw []byte) ([]nftCounterValue, error) {
	var values []nftCounterValue
	var table, counter string
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
		case fields[0] == "table" && len(fields) >= 3:
			table, counter = strings.Trim(fields[2], `"`), ""
		case fields[0] == "counter" && len(fields) >= 2 && fields[1] != "{":
			counter = strings.Trim(fields[1], `"`)
		case fields[0] == "}":
			counter = ""
		case counter != "":
			for i := 0; i+1 < len(fields); i++ {
				if fields[i] != "bytes" {
					continue
				}
				bytes, err := strconv.ParseInt(fields[i+1], 10, 64)
				if err != nil {
					return nil, fmt.Errorf("解析 nft 计数失败: %q", strings.TrimSpace(line))
				}
				values = append(values, nftCounterValue{table: table, name: counter, bytes: bytes})
				counter = ""
				break
			}
		}
	}
	return values, nil
}

func parseCounters(raw []byte) ([]Counter, []UserCounter, error) {
	values, err := parseCounterListing(raw)
	if err != nil {
		return nil, nil, err
	}
	counters, users := parseCounterValues(values)
	return counters, users, nil
}

func parseCounterValues(values []nftCounterValue) ([]Counter, []UserCounter) {
	var counters []Counter
	var users []UserCounter
	for _, c := range values {
		if !isManagedNftTable(c.table) {
			continue
		}
		parts := strings.Split(c.name, "_")
		if len(parts) == 2 && (parts[0] == "uup" || parts[0] == "udown") {
			if id, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
				users = append(users, UserCounter{ID: id, Dir: parts[0][1:], Bytes: c.bytes})
			}
			continue
		}
		if len(parts) != 3 || (parts[0] != "in" && parts[0] != "out") {
			continue
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		counters = append(counters, Counter{InboundID: id, Proto: parts[2], Dir: parts[0], Bytes: c.bytes})
	}
	return counters, users
}

func newEpoch() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
