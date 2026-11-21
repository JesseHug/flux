package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-gost/x/identity"
)

type instance struct {
	req       ApplyRequest
	binDir    string            // main binary directory
	bins      map[string]string // "" = main binary, else extra name -> path
	version   string
	statsAddr string
	logs      *logRing
	procs     []*process
	err       string
}

// Manager owns every kernel instance on this agent. Apply/Control are
// serialized by applyMu (downloads can take minutes); mu guards the maps so
// Status stays responsive meanwhile.
type Manager struct {
	baseDir string

	applyMu     sync.Mutex
	mu          sync.Mutex
	restoreOnce sync.Once
	liteMode    bool
	instances   map[string]*instance
	nft         *nftCounters
	// lastNftErr is the last nftables sync failure logged (guarded by mu).
	lastNftErr string

	statsQuerier  StatsQuerier
	statsEpoch    string
	statsTotals   map[userCounterKey]int64
	statsStarted  bool
	statsStop     chan struct{}
	statsStopOnce sync.Once
}

// SetLiteMode switches the manager between normal and NAT/low-memory lite mode:
// background GeoData auto-updates are disabled and nftables only applies on demand
// when ports require a relay whitelist or port hopping.
func (m *Manager) SetLiteMode(lite bool) {
	m.mu.Lock()
	m.liteMode = lite
	m.mu.Unlock()
}

// IsLiteMode reports whether this manager runs in lite mode.
func (m *Manager) IsLiteMode() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.liteMode
}

var (
	defaultManager     *Manager
	defaultManagerOnce sync.Once
)

// Default uses FLUX_KERNEL_DIR (legacy FLYX_KERNEL_DIR / FLVX_KERNEL_DIR are accepted), or
// the kernels directory alongside the running agent.
func Default() *Manager {
	defaultManagerOnce.Do(func() {
		dir := strings.TrimSpace(identity.Env("FLUX_KERNEL_DIR", "FLYX_KERNEL_DIR", "FLVX_KERNEL_DIR"))
		if dir == "" {
			dir = filepath.Join(identity.DataDir(), "kernels")
		}
		defaultManager = NewManager(dir)
	})
	return defaultManager
}

func NewManager(baseDir string) *Manager {
	return &Manager{
		baseDir:     baseDir,
		instances:   make(map[string]*instance),
		nft:         newNftCounters(baseDir),
		statsEpoch:  newEpoch(),
		statsTotals: make(map[userCounterKey]int64),
		statsStop:   make(chan struct{}),
	}
}

func (m *Manager) statePath() string { return filepath.Join(m.baseDir, "state.json") }
func (m *Manager) runDir(t string) string {
	return filepath.Join(m.baseDir, t, "run")
}
func (m *Manager) binDir(t, version string) string {
	return filepath.Join(m.baseDir, t, "bin", version)
}

// Restore relaunches the kernels recorded by the last Apply so they come back
// after an agent restart even while the panel is unreachable.
func (m *Manager) Restore() {
	m.restoreOnce.Do(func() {
		m.restore()
		if !m.IsLiteMode() {
			m.startGeoUpdater()
			m.startStatsCollector()
		}
	})
}

func (m *Manager) restore() {
	b, err := os.ReadFile(m.statePath())
	if err != nil {
		return
	}
	var reqs []ApplyRequest
	if err := json.Unmarshal(b, &reqs); err != nil {
		fmt.Printf("⚠️ 内核状态文件损坏: %v\n", err)
		return
	}
	for _, req := range reqs {
		if _, err := m.Apply(req); err != nil {
			fmt.Printf("⚠️ 恢复内核 %s 失败: %v\n", req.Type, err)
		}
	}
}

func (m *Manager) persist() {
	m.mu.Lock()
	reqs := make([]ApplyRequest, 0, len(m.instances))
	for _, inst := range m.instances {
		if inst.req.Type != "" {
			reqs = append(reqs, inst.req)
		}
	}
	m.mu.Unlock()
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].Type < reqs[j].Type })
	b, err := json.Marshal(reqs)
	if err != nil {
		return
	}
	if err := os.MkdirAll(m.baseDir, 0o700); err != nil {
		return
	}
	_ = writeFileAtomic(m.statePath(), b, 0o600)
}

func (m *Manager) allPorts() []Port {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ports []Port
	for _, inst := range m.instances {
		ports = append(ports, inst.req.Ports...)
	}
	return ports
}

func (m *Manager) allUserMarks() []int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ids []int64
	for _, inst := range m.instances {
		ids = append(ids, inst.req.UserMarks...)
	}
	return ids
}

func portsNeedNft(ports []Port) bool {
	for _, p := range ports {
		if p.Allow != nil || (p.HopFrom > 0 && p.HopTo > p.HopFrom) {
			return true
		}
	}
	return false
}

// syncNft installs access rules before any protected process starts. Counter
// failures remain non-fatal for kernels without a whitelist.
func (m *Manager) syncNft() error {
	ports := m.allPorts()
	users := m.allUserMarks()
	if m.IsLiteMode() && !portsNeedNft(ports) {
		if !m.nft.hasState() {
			return nil
		}
		// Previously created whitelist/hop rules are no longer needed: delete them.
		ports = nil
		users = nil
	}
	err := m.nft.sync(ports, users)
	if err == nil {
		m.mu.Lock()
		m.lastNftErr = ""
		m.mu.Unlock()
		return nil
	}
	m.mu.Lock()
	// The same failure repeats on every apply (e.g. no permission in a
	// container); log it once per kernel instead of on every change.
	logIt := err.Error() != m.lastNftErr
	m.lastNftErr = err.Error()
	var guarded []*instance
	for _, inst := range m.instances {
		if logIt {
			inst.logs.add("[flux] nftables 规则同步失败: " + err.Error())
		}
		if !inst.req.Enabled {
			continue
		}
		for _, port := range inst.req.Ports {
			if port.Allow != nil {
				guarded = append(guarded, inst)
				break
			}
		}
	}
	m.mu.Unlock()
	if len(guarded) == 0 {
		return nil
	}
	err = fmt.Errorf("入站白名单同步失败，已停止受保护内核: %w", err)
	for _, inst := range guarded {
		m.stopProcesses(inst)
		m.setErr(inst, err)
	}
	return err
}

// ensureBinary installs a binary into dir unless a runnable copy is already
// there. A copy that cannot run here (wrong CPU, missing dynamic loader) is
// removed and installed again, so a bad download does not stick forever.
func (m *Manager) ensureBinary(inst *instance, label string, d *Download, dir string) (string, error) {
	path := filepath.Join(dir, d.BinName)
	if _, err := os.Stat(path); err == nil {
		bad := checkRunnable(path)
		if bad == nil {
			return path, nil
		}
		inst.logs.add(fmt.Sprintf("[flux] 已安装的 %s 无法在本机运行（%v），重新安装", label, bad))
		if err := os.RemoveAll(dir); err != nil {
			return "", fmt.Errorf("删除无法运行的 %s 失败: %v", label, err)
		}
	}
	inst.logs.add(fmt.Sprintf("[flux] 安装 %s", label))
	if err := install(d, dir); err != nil {
		return "", fmt.Errorf("安装 %s 失败: %v", label, err)
	}
	if bad := checkRunnable(path); bad != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("安装的 %s 无法在本机运行: %v", label, bad)
	}
	inst.logs.add(fmt.Sprintf("[flux] %s 安装完成", label))
	return path, nil
}

// Apply installs (if needed), writes the configuration and makes the
// processes match req.Enabled. Unchanged running kernels are left alone.
func (m *Manager) Apply(req ApplyRequest) (State, error) {
	if err := req.validate(); err != nil {
		return State{}, err
	}
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.Lock()
	inst := m.instances[req.Type]
	if inst == nil {
		inst = &instance{logs: &logRing{}}
		m.instances[req.Type] = inst
	}
	m.mu.Unlock()

	binDir := m.binDir(req.Type, req.Version)
	bins := map[string]string{}
	mainBin, err := m.ensureBinary(inst, req.Type+" "+req.Version, req.Download, binDir)
	if err != nil {
		m.setErr(inst, err)
		return m.state(req.Type, false), err
	}
	bins[""] = mainBin
	for _, e := range req.Extras {
		dir := filepath.Join(m.baseDir, req.Type, "bin", "extra-"+e.Name+"-"+e.Version)
		path, err := m.ensureBinary(inst, e.Name+" "+e.Version, e.Download, dir)
		if err != nil {
			m.setErr(inst, err)
			return m.state(req.Type, false), err
		}
		bins[e.Name] = path
	}

	runDir := m.runDir(req.Type)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		m.setErr(inst, err)
		return m.state(req.Type, false), err
	}
	prevFiles := readFiles(runDir, req.Files)

	m.mu.Lock()
	changed := inst.req.Hash != req.Hash || inst.bins[""] != mainBin || len(inst.bins) != len(bins)
	running := len(inst.procs) > 0
	for _, p := range inst.procs {
		if r, _, _, _, _ := p.snapshot(); !r {
			running = false
		}
	}
	prev := prevApply{req: inst.req, binDir: inst.binDir, bins: inst.bins, version: inst.version, statsAddr: inst.statsAddr, files: prevFiles}
	statsAddr := inst.statsAddr
	m.mu.Unlock()

	if !req.Enabled || changed || !running {
		m.stopProcesses(inst)
	}
	filesToWrite := req.Files
	if !req.Enabled {
		statsAddr = ""
	} else if changed || !running || statsAddr == "" {
		statsAddr, filesToWrite = resolveStatsAddr(req, filesToWrite)
	}
	fix := adapters[req.Type].fixFile
	for _, f := range filesToWrite {
		content := f.Content
		if fix != nil {
			content = fix(f.Name, content)
		}
		if err := writeFileAtomic(filepath.Join(runDir, f.Name), []byte(content), 0o600); err != nil {
			m.setErr(inst, err)
			return m.state(req.Type, false), err
		}
	}

	m.mu.Lock()
	inst.req = req
	inst.binDir = binDir
	inst.bins = bins
	inst.statsAddr = statsAddr
	inst.err = ""
	needVersion := inst.version == "" || changed
	m.mu.Unlock()
	if needVersion {
		v := detectVersion(mainBin, adapters[req.Type].versionArgs)
		m.mu.Lock()
		inst.version = v
		m.mu.Unlock()
	}

	if err := m.syncNft(); err != nil {
		m.persist()
		return m.state(req.Type, false), err
	}
	if req.Enabled && (changed || !running) {
		m.startProcesses(inst)
		time.Sleep(startupProbeIn)
	}
	m.startStatsCollector()

	var failure error
	if req.Enabled && !m.state(req.Type, false).Running {
		failed := lastLines(inst.logs, 5)
		failure = fmt.Errorf("内核启动失败: %s", failed)
		if running && prev.req.Enabled {
			// It ran before this change: keep the node serving on the
			// previous config instead of leaving it down.
			if err := m.rollback(inst, runDir, prev); err != nil {
				failure = fmt.Errorf("恢复上一次配置失败: %w", err)
				m.setErr(inst, failure)
			} else if m.state(req.Type, false).Running {
				failure = fmt.Errorf("新配置无法启动，已恢复上一次的配置: %s", failed)
				m.setErr(inst, failure)
			}
		}
	}

	m.persist()
	return m.state(req.Type, false), failure
}

// prevApply is what an instance ran before an apply, to go back to when the
// new config does not start.
type prevApply struct {
	req       ApplyRequest
	binDir    string
	bins      map[string]string
	version   string
	statsAddr string
	files     map[string][]byte
}

func (m *Manager) rollback(inst *instance, runDir string, prev prevApply) error {
	m.stopProcesses(inst)
	for name, content := range prev.files {
		path := filepath.Join(runDir, name)
		if content == nil {
			_ = os.Remove(path)
		} else {
			_ = writeFileAtomic(path, content, 0o600)
		}
	}
	m.mu.Lock()
	inst.req = prev.req
	inst.binDir = prev.binDir
	inst.bins = prev.bins
	inst.version = prev.version
	inst.statsAddr = prev.statsAddr
	m.mu.Unlock()
	if err := m.syncNft(); err != nil {
		return err
	}
	m.startProcesses(inst)
	time.Sleep(startupProbeIn)
	return nil
}

// readFiles returns the current contents of the files an apply is about to
// overwrite; nil marks a file that does not exist yet.
func readFiles(dir string, files []File) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f.Name))
		if err != nil {
			b = nil
		}
		out[f.Name] = b
	}
	return out
}

func expandArgs(args []string, runDir, binDir string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		a = strings.ReplaceAll(a, "{RUN_DIR}", runDir)
		out[i] = strings.ReplaceAll(a, "{BIN_DIR}", binDir)
	}
	return out
}

func lastLines(r *logRing, n int) string {
	lines := r.tail(n)
	if len(lines) == 0 {
		return "无输出"
	}
	return strings.Join(lines, " | ")
}

func (m *Manager) setErr(inst *instance, err error) {
	m.mu.Lock()
	inst.err = err.Error()
	m.mu.Unlock()
	inst.logs.add("[flux] " + err.Error())
}

// startProcesses launches every process of the instance's current request.
func (m *Manager) startProcesses(inst *instance) {
	m.mu.Lock()
	specs := inst.req.processSpecs()
	runDir := m.runDir(inst.req.Type)
	bins := inst.bins
	m.mu.Unlock()

	env := kernelProcessEnv(inst.req.Type)
	procs := make([]*process, 0, len(specs))
	for _, spec := range specs {
		bin := bins[spec.Bin]
		prefix := ""
		if len(specs) > 1 {
			prefix = "[" + spec.Name + "] "
		}
		p := newProcess(bin, expandArgs(spec.Args, runDir, filepath.Dir(bin)), runDir, inst.logs)
		p.prefix = prefix
		p.env = env
		procs = append(procs, p)
	}
	m.mu.Lock()
	inst.procs = procs
	m.mu.Unlock()
	for _, p := range procs {
		p.start()
	}
}

func (m *Manager) stopProcesses(inst *instance) {
	m.drainInstanceStats(inst)
	m.mu.Lock()
	procs := inst.procs
	inst.procs = nil
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func(p *process) {
			defer wg.Done()
			p.stop()
		}(p)
	}
	wg.Wait()
}

// Control handles start / stop / restart / remove for an applied kernel.
func (m *Manager) Control(kernelType, action string) (State, error) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()

	m.mu.Lock()
	inst := m.instances[kernelType]
	m.mu.Unlock()

	if action == "remove" {
		if inst != nil {
			m.stopProcesses(inst)
			m.mu.Lock()
			delete(m.instances, kernelType)
			m.mu.Unlock()
		}
		if _, ok := adapters[kernelType]; ok {
			_ = os.RemoveAll(filepath.Join(m.baseDir, kernelType))
		}
		m.persist()
		if err := m.syncNft(); err != nil {
			return State{Type: kernelType}, err
		}
		return State{Type: kernelType}, nil
	}
	if inst == nil {
		return State{Type: kernelType}, fmt.Errorf("内核 %s 未部署", kernelType)
	}

	switch action {
	case "stop":
		m.stopProcesses(inst)
		m.mu.Lock()
		inst.req.Enabled = false
		m.mu.Unlock()
	case "start", "restart":
		m.stopProcesses(inst)
		m.mu.Lock()
		inst.req.Enabled = true
		if inst.statsAddr == "" && inst.req.StatsAddr != "" {
			inst.statsAddr = inst.req.StatsAddr
		}
		inst.err = ""
		m.mu.Unlock()
		if err := m.syncNft(); err != nil {
			m.persist()
			return m.state(kernelType, false), err
		}
		m.startProcesses(inst)
		time.Sleep(startupProbeIn)
	default:
		return State{}, fmt.Errorf("未知操作: %s", action)
	}
	m.persist()
	return m.state(kernelType, false), nil
}

func (m *Manager) state(kernelType string, withLogs bool) State {
	m.mu.Lock()
	defer m.mu.Unlock()
	inst := m.instances[kernelType]
	st := State{Type: kernelType}
	if inst == nil {
		return st
	}
	st.Enabled = inst.req.Enabled
	st.ConfigHash = inst.req.Hash
	st.Version = inst.version
	st.Error = inst.err
	if len(inst.procs) > 0 {
		st.Running = true
		for i, p := range inst.procs {
			running, pid, restarts, lastErr, startedAt := p.snapshot()
			if i == 0 {
				st.Pid, st.StartedAt = pid, startedAt
			}
			st.Restarts += restarts
			if !running {
				st.Running = false
				if st.Error == "" && lastErr != "" {
					st.Error = p.prefix + lastErr + ": " + lastLines(inst.logs, 3)
				}
			}
		}
	}
	if withLogs {
		st.Logs = inst.logs.tail(logRingSize)
	}
	return st
}

// Status reports every kernel plus the nft traffic counters. logsFor selects
// one kernel whose recent output is included.
func (m *Manager) Status(logsFor string) Status {
	if !m.IsLiteMode() && m.hasStatsQuerier() {
		m.drainAllStats()
	}
	m.mu.Lock()
	types := make([]string, 0, len(m.instances))
	for t := range m.instances {
		types = append(types, t)
	}
	m.mu.Unlock()
	sort.Strings(types)

	out := Status{Kernels: make([]State, 0, len(types))}
	for _, t := range types {
		out.Kernels = append(out.Kernels, m.state(t, t == logsFor))
	}
	if !m.IsLiteMode() && m.hasStatsQuerier() {
		out.StatsCapable = true
		out.StatsEpoch, out.StatsUsers = m.snapshotStatsUsers()
	}
	if m.IsLiteMode() {
		if err := nftAvailable(); err != nil {
			out.NftError = nftUnavailableText(err)
		} else {
			m.mu.Lock()
			lastErr := m.lastNftErr
			m.mu.Unlock()
			if lastErr != "" {
				out.NftError = lastErr
			} else {
				out.NftAvailable = true
			}
		}
		return out
	}
	if len(m.allPorts()) == 0 && !m.nft.hasOffsets() {
		if err := nftAvailable(); err != nil {
			out.NftError = nftUnavailableText(err)
		} else {
			out.NftAvailable = true
		}
		return out
	}
	counters, users, epoch, err := m.nft.read()
	if err != nil {
		out.NftError = err.Error()
		return out
	}
	out.NftAvailable = true
	out.Counters = counters
	out.Users = users
	out.Epoch = epoch
	return out
}

// Shutdown stops every kernel process; the persisted state is kept so the
// next agent start restores them.
// Uninstall stops every kernel and removes the agent's nftables tables, whose
// counters, whitelists and port hopping would otherwise outlive the agent.
func (m *Manager) Uninstall() error {
	m.Shutdown()
	return m.nft.removeTables()
}

func (m *Manager) Shutdown() {
	m.statsStopOnce.Do(func() {
		close(m.statsStop)
	})
	m.mu.Lock()
	insts := make([]*instance, 0, len(m.instances))
	for _, inst := range m.instances {
		insts = append(insts, inst)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, inst := range insts {
		wg.Add(1)
		go func(inst *instance) {
			defer wg.Done()
			m.stopProcesses(inst)
		}(inst)
	}
	wg.Wait()
}

func detectVersion(bin string, args []string) string {
	if len(args) == 0 {
		return ""
	}
	cmd := exec.Command(bin, args...)
	done := make(chan struct{})
	var out []byte
	go func() {
		out, _ = cmd.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-done
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if len(line) > 100 {
		line = line[:100]
	}
	return line
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
