package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-gost/x/identity"
	"github.com/go-gost/x/kernel"
)

const uninstallMarker = ".uninstalled"

var uninstallStarted atomic.Bool

func agentUninstalled(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, uninstallMarker))
	return err == nil
}

func idleAfterUninstall() {
	fmt.Println("ℹ️ 面板已删除这台服务器并卸载了 agent，不再连接面板。")
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	os.Exit(0)
}

func (r *liteReporter) handleUninstallAgent() error {
	if !uninstallStarted.CompareAndSwap(false, true) {
		return nil
	}
	go func() {
		time.Sleep(time.Second)
		runUninstall()
	}()
	return nil
}

func runUninstall() {
	fmt.Println("🗑️ 面板已删除此服务器，开始卸载轻量 agent...")
	_ = kernel.Default().Uninstall()
	dir := identity.DataDir()
	binaryPath := identity.BinaryPath()

	if identity.Env("FLUX_AGENT_CONTAINER", "FLYX_AGENT_CONTAINER") == "1" {
		_ = os.WriteFile(filepath.Join(dir, uninstallMarker), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
		entries, _ := os.ReadDir(dir)
		binName := filepath.Base(binaryPath)
		for _, entry := range entries {
			switch entry.Name() {
			case uninstallMarker, binName, ".image_version":
				continue
			}
			_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
		}
		os.Exit(0)
		return
	}

	name := filepath.Base(binaryPath)
	qName := shellQuote(name)
	qDir := shellQuote(dir)
	script := fmt.Sprintf(
		"sleep 1 && "+
			"(if command -v rc-service >/dev/null 2>&1 && [ -f /etc/alpine-release ]; then rc-service %s stop >/dev/null 2>&1 || true; rc-update del %s default >/dev/null 2>&1 || true; rm -f /etc/init.d/%s; "+
			"else systemctl stop %s >/dev/null 2>&1 || true; systemctl disable %s >/dev/null 2>&1 || true; rm -f /etc/systemd/system/%s.service; systemctl daemon-reload >/dev/null 2>&1 || true; fi) && "+
			"rm -rf %s",
		qName, qName, name, qName, qName, name, qDir,
	)
	_ = startDetachedCommand(script)
}
