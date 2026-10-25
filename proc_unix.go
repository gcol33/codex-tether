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
// stopped through forwardSignals and configureChild instead.
func killChildrenOnExit() error { return nil }

// forwardSignals passes SIGTERM and SIGHUP on to p, so the app stopping its
// CLI also stops the process behind it. SIGINT is caught and dropped: a
// terminal already delivers Ctrl-C to the whole foreground process group.
// The returned function stops the forwarding.
func forwardSignals(p *os.Process) func() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case s := <-ch:
				if s != syscall.SIGINT {
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
