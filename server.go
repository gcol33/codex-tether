package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Each shared server publishes server-<tether pid>.json and authenticates
// with its own token-<tether pid>, so servers started close together never
// share or overwrite each other's files.
const (
	serverFilePrefix = "server-"
	tokenFilePrefix  = "token-"
)

type endpoint struct {
	URL         string `json:"url"`
	TokenFile   string `json:"token_file"`
	RealCLI     string `json:"real_cli"`
	TetherPID   int    `json:"tether_pid"`
	ServerPID   int    `json:"server_pid"`
	ServerStart string `json:"server_start,omitempty"`
	Started     string `json:"started"`
}

func serveShared(real string, args, env []string, stateDir string) int {
	cleanStaleServers(stateDir)
	id := strconv.Itoa(os.Getpid())
	tokenFile := filepath.Join(stateDir, tokenFilePrefix+id)
	token, err := newToken(tokenFile)
	if err != nil {
		log.Printf("token: %v; running the app-server unshared", err)
		return passthrough(real, args, env)
	}
	defer os.Remove(tokenFile)
	port, err := freePort()
	if err != nil {
		log.Printf("port: %v; running the app-server unshared", err)
		return passthrough(real, args, env)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	url := "ws://" + addr

	srvArgs := append(append([]string{}, args...),
		"--listen", url, "--ws-auth", "capability-token", "--ws-token-file", tokenFile)
	srv := exec.Command(real, srvArgs...)
	srv.Env = env
	out := teeWriter{os.Stderr, log.Writer()}
	srv.Stdout, srv.Stderr = out, out
	// Processes the server starts may inherit its output pipe and outlive it;
	// Wait stops copying from the pipe this long after the server exits.
	srv.WaitDelay = 2 * time.Second
	configureServer(srv)
	if err := srv.Start(); err != nil {
		log.Printf("start server: %v; running the app-server unshared", err)
		return passthrough(real, args, env)
	}
	defer forwardSignals(srv.Process, true)()
	defer stopServer(srv.Process)
	log.Printf("server pid %d on %s", srv.Process.Pid, url)

	srvDone := make(chan int, 1)
	go func() {
		err := srv.Wait()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			code = 1
		}
		log.Printf("server exited: %d", code)
		srvDone <- code
	}()

	conn, err := dialUntilReady(addr, token, 60*time.Second, srvDone)
	if err != nil {
		// Nothing has been read from the app yet, so it can still be served
		// by a plain stdio app-server.
		log.Printf("connect: %v; running the app-server unshared", err)
		stopServer(srv.Process)
		<-srvDone
		return passthrough(real, args, env)
	}

	epPath := filepath.Join(stateDir, serverFilePrefix+id+".json")
	ep := endpoint{URL: url, TokenFile: tokenFile, RealCLI: real, TetherPID: os.Getpid(),
		ServerPID: srv.Process.Pid, ServerStart: processStart(srv.Process.Pid),
		Started: time.Now().Format(time.RFC3339Nano)}
	if b, err := json.MarshalIndent(ep, "", "  "); err == nil {
		if err := writeFileAtomic(epPath, b); err != nil {
			log.Printf("publish endpoint: %v", err)
		}
	}
	defer os.Remove(epPath)

	select {
	case why := <-bridge(conn):
		log.Printf("bridge closed: %s", why)
		conn.close()
		stopServer(srv.Process)
		<-srvDone
		return 0
	case code := <-srvDone:
		conn.close()
		return code
	}
}

// cleanStaleServers removes the files of servers whose codex-tether is gone,
// stopping the server too where it can outlive its codex-tether (macOS).
func cleanStaleServers(stateDir string) {
	files, _ := filepath.Glob(filepath.Join(stateDir, serverFilePrefix+"*.json"))
	files = append(files, filepath.Join(stateDir, "endpoint.json"))
	for _, f := range files {
		ep, err := readEndpoint(f)
		if err != nil {
			continue
		}
		if processImage(ep.TetherPID) != "" {
			continue
		}
		stopOrphanedServer(ep)
		_ = os.Remove(f)
		if ep.TokenFile != "" {
			_ = os.Remove(ep.TokenFile)
		}
	}
	_ = os.Remove(filepath.Join(stateDir, "token"))
	tokens, _ := filepath.Glob(filepath.Join(stateDir, tokenFilePrefix+"*"))
	for _, t := range tokens {
		pid, err := strconv.Atoi(strings.TrimPrefix(filepath.Base(t), tokenFilePrefix))
		if err == nil && processImage(pid) == "" {
			_ = os.Remove(t)
		}
	}
}

func readEndpoint(path string) (endpoint, error) {
	var ep endpoint
	b, err := os.ReadFile(path)
	if err != nil {
		return ep, err
	}
	if err := json.Unmarshal(b, &ep); err != nil {
		return ep, fmt.Errorf("bad %s: %v", filepath.Base(path), err)
	}
	return ep, nil
}

// liveEndpoint returns the most recently started published server that
// answers /readyz, with its token.
func liveEndpoint(stateDir string) (*endpoint, string, error) {
	files, _ := filepath.Glob(filepath.Join(stateDir, serverFilePrefix+"*.json"))
	var eps []endpoint
	for _, f := range files {
		if ep, err := readEndpoint(f); err == nil {
			eps = append(eps, ep)
		}
	}
	if len(eps) == 0 {
		return nil, "", errors.New("no shared server (is the Codex app running with codex-tether?)")
	}
	sort.Slice(eps, func(i, j int) bool { return eps[i].Started > eps[j].Started })
	var last error
	client := http.Client{Timeout: 2 * time.Second}
	for i := range eps {
		ep := &eps[i]
		resp, err := client.Get("http://" + strings.TrimPrefix(ep.URL, "ws://") + "/readyz")
		if err != nil {
			last = fmt.Errorf("shared server at %s not answering: %v", ep.URL, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			last = fmt.Errorf("shared server at %s not ready: %s", ep.URL, resp.Status)
			continue
		}
		token, err := os.ReadFile(ep.TokenFile)
		if err != nil {
			last = err
			continue
		}
		return ep, strings.TrimSpace(string(token)), nil
	}
	return nil, "", last
}

func printStatus(stateDir string) int {
	fmt.Printf("codex-tether:  %s\n", version)
	ep, _, err := liveEndpoint(stateDir)
	if err != nil {
		fmt.Println(err)
		return 1
	}
	fmt.Printf("shared server: %s\nreal cli:      %s\nserver pid:    %d\nstarted:       %s\n",
		ep.URL, ep.RealCLI, ep.ServerPID, ep.Started)
	return 0
}

// attach bridges stdio to the shared server.
func attach(stateDir string) int {
	ep, token, err := liveEndpoint(stateDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-tether:", err)
		return 1
	}
	conn, err := dialWS(strings.TrimPrefix(ep.URL, "ws://"), token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-tether:", err)
		return 1
	}
	why := <-bridge(conn)
	log.Printf("attach closed: %s", why)
	conn.close()
	return 0
}

// bridge pumps newline-delimited JSON on stdio to and from WebSocket text
// messages; the channel receives the reason the first direction stopped.
func bridge(conn *wsConn) chan string {
	done := make(chan string, 2)
	go func() { // stdin -> server
		r := bufio.NewReaderSize(os.Stdin, 1<<20)
		first := true
		for {
			line, err := r.ReadBytes('\n')
			line = trimEOL(line)
			if first {
				// .NET writers prefix redirected UTF-8 stdin with a byte order mark.
				line = bytes.TrimPrefix(line, []byte("\xEF\xBB\xBF"))
				first = false
			}
			if len(line) > 0 {
				if werr := conn.writeText(line); werr != nil {
					done <- "write: " + werr.Error()
					return
				}
			}
			if err != nil {
				done <- "stdin: " + err.Error()
				return
			}
		}
	}()
	go func() { // server -> stdout
		w := bufio.NewWriterSize(os.Stdout, 1<<20)
		for {
			msg, err := conn.readMessage()
			if err != nil {
				done <- "read: " + err.Error()
				return
			}
			w.Write(msg)
			w.WriteByte('\n')
			if err := w.Flush(); err != nil {
				done <- "stdout: " + err.Error()
				return
			}
		}
	}()
	return done
}

// teeWriter writes to every writer and never fails, so a closed stderr or an
// unwritable log does not stop the server's output from being drained.
type teeWriter []io.Writer

func (t teeWriter) Write(p []byte) (int, error) {
	for _, w := range t {
		_, _ = w.Write(p)
	}
	return len(p), nil
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// newToken writes a fresh 32-byte random token to path, readable by the
// current user only.
func newToken(path string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	_ = os.Remove(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	if err := restrictToCurrentUser(path); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	_, err = f.WriteString(tok)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	return tok, nil
}

// writeFileAtomic replaces path with data so that readers see either the old
// or the new content, never a partial file.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func dialUntilReady(addr, token string, timeout time.Duration, srvDone chan int) (*wsConn, error) {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		select {
		case code := <-srvDone:
			srvDone <- code
			return nil, fmt.Errorf("server exited (%d) before accepting connections", code)
		default:
		}
		c, err := dialWS(addr, token)
		if err == nil {
			return c, nil
		}
		last = err
		time.Sleep(150 * time.Millisecond)
	}
	return nil, fmt.Errorf("timed out: %v", last)
}
