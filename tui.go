package main

import (
	"fmt"
	"os"
)

const tokenEnvVar = "CODEX_TETHER_TOKEN"

// runTUI starts the Codex TUI. With a live shared server it attaches with
// --remote, using the app's own CLI so client and server versions match;
// otherwise it runs that CLI locally.
func runTUI(stateDir string, args []string) int {
	ep, token, err := liveEndpoint(stateDir)
	if err != nil || os.Getenv(disableVar) == "1" {
		real, rerr := realCLI()
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "codex-tether:", rerr)
			return 1
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "codex-tether: %v; running locally\n", err)
		}
		return passthrough(real, args, os.Environ())
	}
	fmt.Fprintf(os.Stderr, "codex-tether: attached to the desktop app server (%s)\n", ep.URL)
	env := append(os.Environ(), tokenEnvVar+"="+token)
	return passthrough(ep.RealCLI, withRemote(args, ep.URL), env)
}

// withRemote adds the --remote flags ahead of a `--` separator, if any, so
// they stay options rather than becoming part of a prompt.
func withRemote(args []string, url string) []string {
	remote := []string{"--remote", url, "--remote-auth-token-env", tokenEnvVar}
	for i, a := range args {
		if a == "--" {
			out := append(append(append([]string{}, args[:i]...), remote...), args[i:]...)
			return out
		}
	}
	return append(append([]string{}, args...), remote...)
}
