//go:build !windows

package main

import "os"

func runningAsRoot() bool {
	return os.Geteuid() == 0
}
