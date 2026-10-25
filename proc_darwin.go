package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// configureChild is a no-op on macOS, which has no parent-death signal; a
// server left behind by a killed codex-tether is stopped by the next one
// (stopStaleServer).
func configureChild(*exec.Cmd) {}

func processImage(pid int) string {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
