package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeCLIVar makes the test binary act as a Codex CLI that rejects --listen
// and exits with code 7 otherwise.
const fakeCLIVar = "CODEX_TETHER_TEST_FAKE_CLI"

func TestMain(m *testing.M) {
	if os.Getenv(fakeCLIVar) == "1" {
		for _, a := range os.Args[1:] {
			if a == "--listen" {
				os.Exit(2)
			}
		}
		os.Exit(7)
	}
	os.Exit(m.Run())
}

func TestServeSharedFallsBackWhenServerFails(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	env := append(os.Environ(), fakeCLIVar+"=1")
	if code := serveShared(self, []string{"app-server"}, env, state); code != 7 {
		t.Fatalf("serveShared = %d, want the unshared server's exit code 7", code)
	}
	left, _ := filepath.Glob(filepath.Join(state, "*"))
	if len(left) != 0 {
		t.Errorf("files left behind: %q", left)
	}
}

func readyServer(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	return "ws://" + l.Addr().String()
}

func publish(t *testing.T, state string, ep endpoint) {
	b, _ := json.Marshal(ep)
	path := filepath.Join(state, serverFilePrefix+strconv.Itoa(ep.TetherPID)+".json")
	if err := writeFileAtomic(path, b); err != nil {
		t.Fatal(err)
	}
}

func TestLiveEndpointPicksNewestLive(t *testing.T) {
	state := t.TempDir()
	tok := filepath.Join(state, tokenFilePrefix+"1")
	if err := os.WriteFile(tok, []byte("t1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := readyServer(t)
	publish(t, state, endpoint{URL: old, TokenFile: tok, TetherPID: 1, Started: "2026-01-01T00:00:00Z"})
	// A newer entry whose server is gone is skipped.
	dead, _ := net.Listen("tcp", "127.0.0.1:0")
	deadURL := "ws://" + dead.Addr().String()
	dead.Close()
	publish(t, state, endpoint{URL: deadURL, TokenFile: tok, TetherPID: 2, Started: "2026-01-02T00:00:00Z"})

	ep, token, err := liveEndpoint(state)
	if err != nil || ep.URL != old || token != "t1" {
		t.Fatalf("liveEndpoint = %+v, %q, %v; want %s", ep, token, err, old)
	}

	newer := readyServer(t)
	publish(t, state, endpoint{URL: newer, TokenFile: tok, TetherPID: 3, Started: "2026-01-03T00:00:00Z"})
	if ep, _, err := liveEndpoint(state); err != nil || ep.URL != newer {
		t.Fatalf("liveEndpoint = %+v, %v; want %s", ep, err, newer)
	}
}

func TestLiveEndpointNone(t *testing.T) {
	if _, _, err := liveEndpoint(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no shared server") {
		t.Fatalf("err = %v", err)
	}
}

// deadPID returns a PID that no running process has.
func deadPID(t *testing.T) int {
	for pid := 999983; pid > 900000; pid -= 2 {
		if processImage(pid) == "" {
			return pid
		}
	}
	t.Skip("no unused PID found")
	return 0
}

func TestCleanStaleServers(t *testing.T) {
	state := t.TempDir()
	gone := deadPID(t)
	tok := filepath.Join(state, tokenFilePrefix+strconv.Itoa(gone))
	os.WriteFile(tok, []byte("x"), 0o600)
	publish(t, state, endpoint{URL: "ws://127.0.0.1:1", TokenFile: tok, TetherPID: gone, ServerPID: gone})
	live := os.Getpid()
	liveTok := filepath.Join(state, tokenFilePrefix+strconv.Itoa(live))
	os.WriteFile(liveTok, []byte("y"), 0o600)
	publish(t, state, endpoint{URL: "ws://127.0.0.1:2", TokenFile: liveTok, TetherPID: live})
	legacy := filepath.Join(state, "endpoint.json")
	os.WriteFile(legacy, []byte(`{"tether_pid":`+strconv.Itoa(gone)+`}`), 0o600)

	cleanStaleServers(state)

	for _, p := range []string{tok, legacy, filepath.Join(state, serverFilePrefix+strconv.Itoa(gone)+".json")} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s not removed", filepath.Base(p))
		}
	}
	for _, p := range []string{liveTok, filepath.Join(state, serverFilePrefix+strconv.Itoa(live)+".json")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s removed: %v", filepath.Base(p), err)
		}
	}
}

func TestNewToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), tokenFilePrefix+"1")
	a, err := newToken(path)
	if err != nil || len(a) != 64 {
		t.Fatalf("newToken = %q, %v", a, err)
	}
	b, err := newToken(path)
	if err != nil || b == a {
		t.Fatalf("second token %q, %v; want a different token", b, err)
	}
	if got, _ := os.ReadFile(path); string(got) != b {
		t.Errorf("file holds %q, want %q", got, b)
	}
}

type failingWriter struct{ n int }

func (f *failingWriter) Write(p []byte) (int, error) {
	f.n++
	return 0, errors.New("closed")
}

func TestTeeWriterIgnoresFailures(t *testing.T) {
	bad := &failingWriter{}
	var good strings.Builder
	w := teeWriter{bad, &good}
	for i := 0; i < 3; i++ {
		if n, err := w.Write([]byte("ab")); n != 2 || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	if good.String() != "ababab" || bad.n != 3 {
		t.Errorf("good %q, bad writes %d", good.String(), bad.n)
	}
}

func TestReadFrameRejectsOversized(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go server.Write([]byte{0x81, 127, 0x7F, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
	if _, _, _, err := readFrame(client); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v", err)
	}
}

func TestRealCLIRejectsSelf(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(realCLIVar, self)
	if _, err := realCLI(t.TempDir(), ""); err == nil {
		t.Fatal("override pointing at codex-tether itself was accepted")
	}
}
