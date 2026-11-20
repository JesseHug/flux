//go:build !linux

package kernel

func kernelProcessEnv(string) []string { return nil }
