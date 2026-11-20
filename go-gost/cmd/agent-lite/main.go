package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/go-gost/x/identity"
	"github.com/go-gost/x/kernel"
)

var version = "dev"

func main() {
	var (
		printVersion bool
		cfgFile      string
	)
	flag.BoolVar(&printVersion, "V", false, "print version")
	flag.StringVar(&cfgFile, "C", "", "configuration file")
	flag.Parse()

	if printVersion {
		fmt.Fprintf(os.Stdout, "flyx-agent-lite %s (%s %s/%s)\n",
			version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	// Keep the Go heap compact on ultra-low-memory NAT nodes (e.g. 128 MiB).
	debug.SetMemoryLimit(16 << 20)
	debug.SetGCPercent(25)
	_ = disableCoreDumps()

	dataDir := identity.DataDir()
	if agentUninstalled(dataDir) {
		idleAfterUninstall()
		return
	}

	configFile := cfgFile
	if configFile == "" {
		configFile = filepath.Join(dataDir, "config.json")
	}
	if err := applyEnvConfig(configFile); err != nil {
		fmt.Printf("❌ 应用环境变量配置失败: %v\n", err)
		os.Exit(1)
	}
	cfg, err := loadConfig(configFile)
	if err != nil {
		fmt.Printf("❌ 加载配置失败: %v\n", err)
		os.Exit(1)
	}

	mgr := kernel.Default()
	mgr.SetLiteMode(true)
	go mgr.Restore()

	fullVersion := fmt.Sprintf("%s-lite (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
	rep := newReporter(cfg.Addr, cfg.Secret, fullVersion)
	rep.Start()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	rep.Stop()
	mgr.Shutdown()
}
