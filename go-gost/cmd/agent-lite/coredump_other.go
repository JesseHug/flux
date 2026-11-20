//go:build !unix

package main

import "os"

func disableCoreDumps() error { return nil }

func terminateSelf() {
	os.Exit(0)
}
