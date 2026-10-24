package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const sandboxPackageFamilyVar = "CODEX_WINDOWS_SANDBOX_PACKAGE_FAMILY"

// realCLI returns the newest codex.exe the desktop app has staged under
// %LOCALAPPDATA%\OpenAI\Codex\bin\<hash>\.
func realCLI() (string, error) {
	if p := strings.TrimSpace(os.Getenv(realCLIVar)); p != "" {
		return p, nil
	}
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenAI", "Codex", "bin", "*", "codex.exe"))
	if len(matches) == 0 {
		return "", errors.New(`no staged codex.exe under %LOCALAPPDATA%\OpenAI\Codex\bin (has the Codex app run once?)`)
	}
	sort.Slice(matches, func(i, j int) bool { return modTime(matches[i]).After(modTime(matches[j])) })
	return matches[0], nil
}

func modTime(p string) time.Time {
	st, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
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

// isStdioAppServer reports whether args launch the plain stdio app-server:
// subcommand `app-server`, no nested subcommand, no explicit transport.
func isStdioAppServer(args []string) bool {
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
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
		case a == "--listen" || strings.HasPrefix(a, "--listen=") || a == "--stdio":
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
	if err := cmd.Run(); err != nil {
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
