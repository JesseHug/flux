package kernel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestDirectFallbackURL(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"https://gcode.hostcentral.cc/https://github.com/SagerNet/sing-box/releases/download/v1.14.4/a.tar.gz", "https://github.com/SagerNet/sing-box/releases/download/v1.14.4/a.tar.gz"},
		{"https://panel.example.com/api/v1/open_api/gh/https://api.github.com/repos/a/b", "https://api.github.com/repos/a/b"},
		{"https://github.com/SagerNet/sing-box/releases/latest", ""},
		{"", ""},
	} {
		if got := directFallbackURL(tc.in); got != tc.want {
			t.Errorf("directFallbackURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestInstallFallsBackToDirectWhenProxyFails(t *testing.T) {
	bin := fakeKernel
	archive := zipWith(t, map[string]string{"xray": bin})
	srv, _ := serveKernel(t, archive)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Rate limit", http.StatusTooManyRequests)
	}))
	t.Cleanup(proxy.Close)

	m := NewManager(t.TempDir())
	req := applyRequest(srv, "{}", false)
	req.Download.URL = proxy.URL + "/" + srv.URL + "/Xray-linux-{ARCH}.zip"
	req.Download.ChecksumURL = proxy.URL + "/" + srv.URL + "/Xray-linux-{ARCH}.zip.dgst"
	req.Version = "1.0.0"
	if _, err := m.Apply(req); err != nil {
		t.Fatalf("Apply should succeed via direct fallback: %v", err)
	}
}

func TestLiteModeOnDemandNftSyncAndStatus(t *testing.T) {
	rec := stubNft(t)
	srv, _ := serveKernel(t, zipWith(t, map[string]string{"xray": fakeKernel}))
	m := NewManager(t.TempDir())
	m.SetLiteMode(true)
	t.Cleanup(m.Shutdown)

	base := applyRequest(srv, "{}", true)
	base.Version = "1.0.0"
	base.Download.ArchMap = map[string]string{runtime.GOARCH: "test"}

	// Plain inbound without whitelist or port hop: no nftables table is written.
	reqPlain := base
	reqPlain.Hash = "plain"
	reqPlain.Ports = []Port{{InboundID: 1, Port: 8388, Network: "tcp,udp"}}
	if st, err := m.Apply(reqPlain); err != nil || !st.Running {
		t.Fatalf("Apply plain failed: err=%v state=%+v", err, st)
	}
	if len(rec.scripts) != 0 {
		t.Fatalf("expected 0 nft scripts for plain lite inbound, got %d", len(rec.scripts))
	}

	// Status in lite mode must not call `nft list counters`.
	var listCountersCalled bool
	origRunner := nftRunner
	nftRunner = func(ctx context.Context, stdin string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "list counters table inet "+nftTable {
			listCountersCalled = true
		}
		return origRunner(ctx, stdin, args...)
	}
	st := m.Status("")
	if !st.NftAvailable || len(st.Counters) != 0 {
		t.Fatalf("unexpected lite status: %+v", st)
	}
	if listCountersCalled {
		t.Fatal("lite mode Status should not poll nft counters")
	}

	// Enabling a relay whitelist installs the nftables guard on demand.
	nftRunner = origRunner
	reqGuard := base
	reqGuard.Hash = "guard"
	reqGuard.Ports = []Port{{InboundID: 1, Port: 8388, Network: "tcp,udp", Allow: []string{"203.0.113.10/32"}}}
	if _, err := m.Apply(reqGuard); err != nil {
		t.Fatalf("Apply guarded failed: %v", err)
	}
	if len(rec.scripts) == 0 || !strings.Contains(rec.scripts[len(rec.scripts)-1], "203.0.113.10") {
		t.Fatalf("expected on-demand whitelist nft script, got %v", rec.scripts)
	}

	// Removing the whitelist cleans up the previously created table.
	reqClean := reqPlain
	reqClean.Hash = "clean"
	if _, err := m.Apply(reqClean); err != nil {
		t.Fatalf("Apply cleanup failed: %v", err)
	}
	if last := rec.scripts[len(rec.scripts)-1]; !strings.HasSuffix(strings.TrimSpace(last), "delete table inet "+nftTable) {
		t.Fatalf("expected table deletion on cleanup, got %q", last)
	}
}
