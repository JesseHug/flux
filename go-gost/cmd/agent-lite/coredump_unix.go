//go:build unix

package main

import (
	"os"
	"syscall"
)

func disableCoreDumps() error {
	return syscall.Setrlimit(syscall.RLIMIT_CORE, &syscall.Rlimit{})
}

func terminateSelf() {
	_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
}
