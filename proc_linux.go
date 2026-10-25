package main

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// configureChild makes the kernel kill the child when codex-tether dies,
// including by SIGKILL, so the server cannot outlive it holding thread locks.
func configureChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

func processImage(pid int) string {
	p, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil {
		return ""
	}
	return p
}
