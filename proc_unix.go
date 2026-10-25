//go:build linux || darwin

package main

import (
	"errors"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

// killChildrenOnExit has no Unix counterpart to a job object; children are
// stopped through forwardSignals, configureChild and configureServer instead.
func killChildrenOnExit() error { return nil }

// releaseChildren has nothing to release on Unix.
func releaseChildren() {}

// forwardSignals passes SIGTERM and SIGHUP on to p, or to p's process group
// when group is set, so the app stopping its CLI also stops the processes
// behind it. SIGINT is caught and dropped: a terminal already delivers Ctrl-C
// to the whole foreground process group. The returned function stops the
// forwarding.
func forwardSignals(p *os.Process, group bool) func() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-ch:
				if s == syscall.SIGINT {
					continue
				}
				if group {
					_ = syscall.Kill(-p.Pid, s.(syscall.Signal))
				} else {
					_ = p.Signal(s)
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// stopServer kills the server and every process left in its process group.
func stopServer(p *os.Process) {
	_ = syscall.Kill(-p.Pid, syscall.SIGKILL)
	_ = p.Kill()
}

// bundledCLI returns the codex binary shipped inside the desktop app whose
// main executable is appExe, searched the way the app itself searches.
func bundledCLI(appExe string) (string, error) {
	if appExe == "" {
		return "", errors.New("no desktop app process to locate its codex binary from")
	}
	for _, c := range appCLICandidates(resourcesDir(appExe, runtime.GOOS)) {
		if p := executableIn(c); p != "" {
			return p, nil
		}
	}
	return "", errors.New("no codex binary in the resources of " + appExe)
}

func restrictToCurrentUser(path string) error {
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		mode = 0o700
	}
	return os.Chmod(path, mode)
}
