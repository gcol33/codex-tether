package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// configureChild makes the kernel kill the child when codex-tether dies,
// including by SIGKILL.
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

// configureServer also puts the server in its own process group, so
// stopServer reaches the processes it starts.
func configureServer(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL, Setpgid: true}
}

// processStart is not recorded on Linux: the parent-death signal already
// stops a server whose codex-tether is gone.
func processStart(int) string { return "" }

func stopOrphanedServer(endpoint) {}

func processImage(pid int) string {
	p, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return ""
	}
	return p
}
