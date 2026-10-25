package main

import (
	"log"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// configureChild is a no-op on macOS, which has no parent-death signal.
func configureChild(*exec.Cmd) {}

// configureServer puts the server in its own process group, so stopServer
// reaches the processes it starts.
func configureServer(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// processStart returns the start time of process pid as ps prints it, which
// tells a recorded server apart from a later process given the same PID.
func processStart(pid int) string {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// stopOrphanedServer stops a server left running by a codex-tether that was
// killed outright, after checking that the recorded PID still runs the
// recorded CLI and started at the recorded time.
func stopOrphanedServer(ep endpoint) {
	if ep.ServerPID <= 0 || ep.ServerStart == "" {
		return
	}
	img := processImage(ep.ServerPID)
	if img == "" || filepath.Clean(img) != filepath.Clean(ep.RealCLI) || processStart(ep.ServerPID) != ep.ServerStart {
		return
	}
	log.Printf("stopping orphaned server pid %d", ep.ServerPID)
	_ = syscall.Kill(-ep.ServerPID, syscall.SIGKILL)
	_ = syscall.Kill(ep.ServerPID, syscall.SIGKILL)
}

func processImage(pid int) string {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
