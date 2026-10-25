// codex-tether stands in for the Codex desktop app's CLI binary
// (CODEX_CLI_PATH) on Windows, macOS and Linux.
//
// When the app starts its stdio app-server, codex-tether starts the real
// app-server on a token-protected loopback WebSocket instead and bridges the
// app's stdio to it. Terminal sessions started with `codex-tether tui` attach
// to that same server with `--remote`, so a thread is live in the app and in
// the terminal at once instead of being locked by whichever opened it first.
// Every other invocation runs the real binary unchanged.
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

const (
	disableVar = "CODEX_TETHER_DISABLE"
	realCLIVar = "CODEX_TETHER_REAL_CLI"
)

const usage = `codex-tether: share the Codex desktop app's server with terminal sessions

Usage:
  codex-tether tui [codex args...]   start the Codex TUI attached to the app's server
                                     (runs locally when the app is not running)
  codex-tether status                print the shared server endpoint, if any
  codex-tether attach                bridge stdin/stdout (JSON-RPC lines) to the server

Installed as CODEX_CLI_PATH, the desktop app invokes it like the codex binary.
`

func main() {
	stateDir := stateDirectory()
	if err := ensurePrivateDir(stateDir); err != nil {
		fmt.Fprintln(os.Stderr, "codex-tether:", err)
	}
	setupLog(stateDir)

	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "tui":
			os.Exit(runTUI(stateDir, args[1:]))
		case "status":
			os.Exit(printStatus(stateDir))
		case "attach":
			os.Exit(attach(stateDir))
		case "-h", "--help", "help":
			if len(args) == 1 {
				fmt.Print(usage)
				return
			}
		}
	}

	// Children die with the tether: when the app terminates "its CLI", the
	// real process tree behind it goes too, and no orphan keeps thread locks.
	if err := killChildrenOnExit(); err != nil {
		log.Printf("job object: %v", err)
	}

	appExe := processImage(os.Getppid())
	real, err := realCLI(stateDir, appExe)
	if err != nil {
		log.Printf("resolve real cli: %v", err)
		fmt.Fprintln(os.Stderr, "codex-tether:", err)
		os.Exit(1)
	}
	env := childEnv(os.Environ(), appExe)

	if os.Getenv(disableVar) == "1" || !isStdioAppServer(args) {
		log.Printf("passthrough: %q", args)
		os.Exit(passthrough(real, args, env))
	}
	log.Printf("shared app-server: %q (real %s)", args, real)
	os.Exit(serveShared(real, args, env, stateDir))
}

// stateDirectory is tether/ inside the Codex home: CODEX_HOME when set,
// otherwise .codex in the user's home directory.
func stateDirectory() string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".codex")
	}
	return filepath.Join(home, "tether")
}

func ensurePrivateDir(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return restrictToCurrentUser(dir)
}

func setupLog(stateDir string) {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	f, err := os.OpenFile(filepath.Join(stateDir, "tether.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		log.SetOutput(io.Discard)
		return
	}
	log.SetOutput(f)
}
