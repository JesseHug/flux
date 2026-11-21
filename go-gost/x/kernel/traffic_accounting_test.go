package kernel

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestNftSyncPreservesOffsetsAcrossTableRebuild(t *testing.T) {
	var mu sync.Mutex
	tableExists := false
	live := map[string]int64{}

	oldRunner, oldAvail := nftRunner, nftAvailable
	nftAvailable = func() error { return nil }
	nftRunner = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		joined := strings.Join(args, " ")
		switch {
		case joined == "list tables":
			if tableExists {
				return []byte("table inet " + nftTable + "\n"), nil
			}
			return nil, nil
		case len(args) >= 1 && args[0] == "-f":
			if strings.Contains(stdin, "delete table inet "+nftTable) {
				tableExists = false
				live = map[string]int64{}
			}
			if strings.Contains(stdin, "table inet "+nftTable+" {") {
				tableExists = true
				for _, line := range strings.Split(stdin, "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "counter ") && strings.HasSuffix(line, "{}") {
						name := strings.TrimSuffix(strings.TrimPrefix(line, "counter "), " {}")
						live[name] = 0
					}
				}
			}
			return nil, nil
		case joined == "list counters table inet "+nftTable:
			if !tableExists {
				return nil, fmt.Errorf("table inet %s does not exist", nftTable)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "table inet %s {\n", nftTable)
			for name, val := range live {
				fmt.Fprintf(&b, "\tcounter %s {\n\t\tpackets 1 bytes %d\n\t}\n", name, val)
			}
			b.WriteString("}\n")
			return []byte(b.String()), nil
		default:
			return nil, nil
		}
	}
	t.Cleanup(func() {
		nftRunner, nftAvailable = oldRunner, oldAvail
	})

	nc := newNftCounters(t.TempDir())
	ports := []Port{{InboundID: 1, Port: 443, Network: "tcp"}}

	if err := nc.sync(ports, []int64{1}); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	epoch1 := nc.state.Epoch
	if epoch1 == "" {
		t.Fatal("expected initial epoch")
	}

	// Accumulate traffic before polling, then add user 2 (rebuilding the table).
	mu.Lock()
	live["uup_1"] = 2_500_000
	live["udown_1"] = 10_000_000
	live["in_1_tcp"] = 3_000_000
	mu.Unlock()

	if err := nc.sync(ports, []int64{1, 2}); err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if nc.state.Epoch != epoch1 {
		t.Fatalf("epoch changed on table rebuild: %q -> %q", epoch1, nc.state.Epoch)
	}

	// Accumulate more traffic in the rebuilt table, then remove user 1.
	mu.Lock()
	live["uup_1"] = 100
	live["udown_1"] = 200
	mu.Unlock()

	if err := nc.sync(ports, []int64{2}); err != nil {
		t.Fatalf("third sync removing user 1: %v", err)
	}

	counters, users, gotEpoch, err := nc.read()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if gotEpoch != epoch1 {
		t.Fatalf("expected continuous epoch %q, got %q", epoch1, gotEpoch)
	}
	userBytes := map[string]int64{}
	for _, u := range users {
		userBytes[fmt.Sprintf("%d:%s", u.ID, u.Dir)] = u.Bytes
	}
	if userBytes["1:up"] != 2_500_100 || userBytes["1:down"] != 10_000_200 {
		t.Fatalf("expected preserved totals for removed user 1, got %v", userBytes)
	}
	if len(counters) == 0 || counters[0].Bytes != 3_000_000 {
		t.Fatalf("expected preserved inbound counter 3000000, got %+v", counters)
	}

	// Removing all ports/users deletes the table, but carried offsets still flush.
	if err := nc.sync(nil, nil); err != nil {
		t.Fatalf("clear sync: %v", err)
	}
	counters, users, _, err = nc.read()
	if err != nil || len(counters) == 0 || len(users) == 0 {
		t.Fatalf("expected carried offsets when table cleared: err=%v counters=%v users=%v", err, counters, users)
	}
}

func TestParseXrayUserStat(t *testing.T) {
	cases := []struct {
		name string
		id   int64
		dir  string
		ok   bool
	}{
		{"user>>>u7@flux>>>traffic>>>uplink", 7, "up", true},
		{"user>>>u42@flux>>>traffic>>>downlink", 42, "down", true},
		{"user>>>u9>>>traffic>>>uplink", 9, "up", true},
		{"inbound>>>in-1>>>traffic>>>uplink", 0, "", false},
		{"user>>>owner>>>traffic>>>uplink", 0, "", false},
		{"user>>>u0@flux>>>traffic>>>uplink", 0, "", false},
	}
	for _, tc := range cases {
		id, dir, ok := ParseXrayUserStat(tc.name)
		if id != tc.id || dir != tc.dir || ok != tc.ok {
			t.Fatalf("ParseXrayUserStat(%q) = (%d,%q,%v), want (%d,%q,%v)", tc.name, id, dir, ok, tc.id, tc.dir, tc.ok)
		}
	}
}

func TestManagerStatsQuerierDrainsAndRespectsLiteMode(t *testing.T) {
	oldRunner, oldAvail := nftRunner, nftAvailable
	nftAvailable = func() error { return nil }
	nftRunner = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
		return nil, nil
	}
	t.Cleanup(func() {
		nftRunner, nftAvailable = oldRunner, oldAvail
	})

	m := NewManager(t.TempDir())
	var mu sync.Mutex
	pending := []UserCounter{{ID: 7, Dir: "up", Bytes: 1200}, {ID: 7, Dir: "down", Bytes: 3400}}
	calls := 0
	m.SetStatsQuerier(func(ctx context.Context, addr string) ([]UserCounter, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		out := append([]UserCounter(nil), pending...)
		pending = nil
		return out, nil
	})
	p := newProcess("true", nil, t.TempDir(), &logRing{})
	close(p.done)
	inst := &instance{
		statsAddr: "127.0.0.1:19850",
		procs:     []*process{p},
		req:       ApplyRequest{Type: "xray", Enabled: true, StatsAddr: "127.0.0.1:19850"},
	}
	m.instances["xray"] = inst

	st := m.Status("")
	if !st.StatsCapable || st.StatsEpoch == "" || len(st.StatsUsers) != 2 {
		t.Fatalf("unexpected normal stats status: %+v", st)
	}
	if st.StatsUsers[0] != (UserCounter{ID: 7, Dir: "down", Bytes: 3400}) ||
		st.StatsUsers[1] != (UserCounter{ID: 7, Dir: "up", Bytes: 1200}) {
		t.Fatalf("unexpected stats users: %+v", st.StatsUsers)
	}

	// Simulate more traffic before process stop: stopProcesses must drain first.
	mu.Lock()
	pending = []UserCounter{{ID: 7, Dir: "down", Bytes: 600}}
	mu.Unlock()
	m.stopProcesses(inst)
	epochAfterStop, usersAfterStop := m.snapshotStatsUsers()
	if epochAfterStop != st.StatsEpoch || usersAfterStop[0].Bytes != 4000 {
		t.Fatalf("expected stopProcesses to drain 600 bytes to 4000, got %+v", usersAfterStop)
	}

	// Lite mode never exposes StatsCapable or calls stats querier.
	beforeCalls := calls
	m.SetLiteMode(true)
	lite := m.Status("")
	if lite.StatsCapable || len(lite.StatsUsers) != 0 || calls != beforeCalls {
		t.Fatalf("lite mode must disable stats collection: %+v calls=%d/%d", lite, calls, beforeCalls)
	}
}
