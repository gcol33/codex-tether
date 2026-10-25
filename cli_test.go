package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResourcesDir(t *testing.T) {
	mac := filepath.Join("Applications", "Codex.app", "Contents", "MacOS", "Codex")
	if got, want := resourcesDir(mac, "darwin"), filepath.Join("Applications", "Codex.app", "Contents", "Resources"); got != want {
		t.Errorf("darwin: got %q, want %q", got, want)
	}
	linux := filepath.Join("opt", "codex", "codex-desktop")
	if got, want := resourcesDir(linux, "linux"), filepath.Join("opt", "codex", "resources"); got != want {
		t.Errorf("linux: got %q, want %q", got, want)
	}
}

func TestExecutableIn(t *testing.T) {
	res := t.TempDir()
	if p := executableIn(filepath.Join(res, "codex")); p != "" {
		t.Errorf("missing file: got %q", p)
	}
	linuxDir := filepath.Join(res, "codex-linux")
	if err := os.Mkdir(linuxDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if p := executableIn(linuxDir); p != "" {
		t.Errorf("empty directory: got %q", p)
	}
	bin := filepath.Join(linuxDir, "codex")
	if err := os.WriteFile(bin, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if p := executableIn(linuxDir); p != bin {
		t.Errorf("directory: got %q, want %q", p, bin)
	}
	var found string
	for _, c := range appCLICandidates(res) {
		if found = executableIn(c); found != "" {
			break
		}
	}
	if found != bin {
		t.Errorf("candidates: got %q, want %q", found, bin)
	}
}

func TestRealCLIOverrideAndRecord(t *testing.T) {
	state := t.TempDir()
	t.Setenv(realCLIVar, "/custom/codex")
	if p, err := realCLI(state, ""); err != nil || p != "/custom/codex" {
		t.Fatalf("override: got %q, %v", p, err)
	}
	t.Setenv(realCLIVar, "")
	bin := filepath.Join(t.TempDir(), "codex")
	if err := os.WriteFile(bin, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, appCLIFile), []byte(bin+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if p, err := realCLI(state, ""); err != nil || p != bin {
		t.Fatalf("recorded: got %q, %v", p, err)
	}
}
