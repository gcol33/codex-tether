package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	sandboxPackageFamilyVar = "CODEX_WINDOWS_SANDBOX_PACKAGE_FAMILY"
	appCLIFile              = "app-cli"
)

// realCLI resolves the Codex binary codex-tether stands in for: the
// CODEX_TETHER_REAL_CLI override, else the desktop app's own CLI (found from
// the app process appExe, or the one recorded the last time the app ran),
// else codex on PATH.
func realCLI(stateDir, appExe string) (string, error) {
	if p := strings.TrimSpace(os.Getenv(realCLIVar)); p != "" {
		if isSelf(p) {
			return "", fmt.Errorf("%s points at codex-tether itself", realCLIVar)
		}
		return p, nil
	}
	recorded := filepath.Join(stateDir, appCLIFile)
	p, bundledErr := bundledCLI(appExe)
	if bundledErr == nil && isSelf(p) {
		bundledErr = fmt.Errorf("the app's CLI at %s is codex-tether itself", p)
	}
	if bundledErr == nil {
		_ = writeFileAtomic(recorded, []byte(p))
		return p, nil
	}
	if b, err := os.ReadFile(recorded); err == nil {
		if p := executableIn(strings.TrimSpace(string(b))); p != "" && !isSelf(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("codex"); err == nil && !isSelf(p) {
		return p, nil
	}
	return "", bundledErr
}

// resourcesDir is the Electron resources directory of an app whose main
// executable is exe: Contents/Resources in a macOS bundle, resources/ beside
// the executable elsewhere.
func resourcesDir(exe, goos string) string {
	if goos == "darwin" {
		return filepath.Join(filepath.Dir(exe), "..", "Resources")
	}
	return filepath.Join(filepath.Dir(exe), "resources")
}

// appCLICandidates lists where the desktop app looks for its bundled CLI,
// in the app's own order.
func appCLICandidates(res string) []string {
	return []string{
		filepath.Join(res, "codex"),
		filepath.Join(res, "app.asar.unpacked", "codex"),
		filepath.Join(res, "codex-linux"),
	}
}

// executableIn returns p, or p/codex when p is a directory, if that is a
// regular file; otherwise "".
func executableIn(p string) string {
	if p == "" {
		return ""
	}
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		p = filepath.Join(p, "codex")
	}
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
		return p
	}
	return ""
}

func isSelf(p string) bool {
	self, err := os.Executable()
	if err != nil {
		return false
	}
	a, errA := filepath.EvalSymlinks(p)
	b, errB := filepath.EvalSymlinks(self)
	return errA == nil && errB == nil && strings.EqualFold(a, b)
}

// packageFamilyFromPath derives an MSIX package family name
// (<Name>_<PublisherId>) from a path inside the package's install directory,
// e.g. ...\WindowsApps\OpenAI.Codex_26.1013.4894.0_x64__2p2nqsd0c76g0\app\ChatGPT.exe.
func packageFamilyFromPath(p string) string {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '\\' || r == '/' })
	for i, part := range parts {
		if !strings.EqualFold(part, "WindowsApps") || i+1 >= len(parts) {
			continue
		}
		full := parts[i+1]
		name, _, ok := strings.Cut(full, "_")
		pub := full[strings.LastIndex(full, "__")+2:]
		if !ok || !strings.Contains(full, "__") || name == "" || pub == "" {
			return ""
		}
		return name + "_" + pub
	}
	return ""
}

// childEnv restores the sandbox package family the app strips from the
// environment when CODEX_CLI_PATH is overridden. The family comes from the
// packaged app that launched us; outside a packaged parent nothing is added.
func childEnv(env []string, parentImage string) []string {
	for _, kv := range env {
		if strings.HasPrefix(strings.ToUpper(kv), sandboxPackageFamilyVar+"=") {
			return env
		}
	}
	if fam := packageFamilyFromPath(parentImage); fam != "" {
		return append(env, sandboxPackageFamilyVar+"="+fam)
	}
	return env
}

// valueFlags take a separate value argument in `codex [global] app-server`.
var valueFlags = map[string]bool{
	"-c": true, "--config": true, "--enable": true, "--disable": true,
	"--startup-client-info": true, "--cloud-skills-snapshot": true,
	"--code-mode-host": true, "--remote": true, "--remote-auth-token-env": true,
	"-m": true, "--model": true, "-p": true, "--profile": true,
	"-C": true, "--cd": true, "-s": true, "--sandbox": true,
	"-a": true, "--ask-for-approval": true,
}

// infoFlags print something and exit instead of starting a server.
var infoFlags = map[string]bool{"-h": true, "--help": true, "-V": true, "--version": true}

// isStdioAppServer reports whether args launch the plain stdio app-server:
// subcommand `app-server`, no nested subcommand, no explicit transport.
func isStdioAppServer(args []string) bool {
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if infoFlags[a] {
			return false
		}
		if valueFlags[a] {
			i++
			continue
		}
		if !strings.HasPrefix(a, "-") {
			break
		}
	}
	if i >= len(args) || args[i] != "app-server" {
		return false
	}
	for j := i + 1; j < len(args); j++ {
		a := args[j]
		switch {
		case a == "--listen" || strings.HasPrefix(a, "--listen=") || a == "--stdio" || infoFlags[a]:
			return false
		case valueFlags[a]:
			j++
		case !strings.HasPrefix(a, "-"):
			return false
		}
	}
	return true
}

func passthrough(real string, args, env []string) int {
	cmd := exec.Command(real, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = env
	configureChild(cmd)
	if err := cmd.Start(); err != nil {
		log.Printf("run %s: %v", real, err)
		fmt.Fprintln(os.Stderr, "codex-tether:", err)
		return 1
	}
	stop := forwardSignals(cmd.Process, false)
	err := cmd.Wait()
	stop()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		log.Printf("run %s: %v", real, err)
		fmt.Fprintln(os.Stderr, "codex-tether:", err)
		return 1
	}
	return 0
}
