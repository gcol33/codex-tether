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
	"strings"
	"time"
)

const endpointFile = "endpoint.json"

type endpoint struct {
	URL       string `json:"url"`
	TokenFile string `json:"token_file"`
	RealCLI   string `json:"real_cli"`
	TetherPID int    `json:"tether_pid"`
	ServerPID int    `json:"server_pid"`
	Started   string `json:"started"`
}

func serveShared(real string, args, env []string, stateDir string) int {
	stopStaleServer(stateDir)
	tokenFile := filepath.Join(stateDir, "token")
	token, err := ensureToken(tokenFile)
	if err != nil {
		log.Printf("token: %v; running the app-server unshared", err)
		return passthrough(real, args, env)
	}
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
	srvLog := io.MultiWriter(os.Stderr, log.Writer())
	srv.Stdout = srvLog
	srv.Stderr = srvLog
	configureChild(srv)
	if err := srv.Start(); err != nil {
		log.Printf("start server: %v; running the app-server unshared", err)
		return passthrough(real, args, env)
	}
	defer forwardSignals(srv.Process)()
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
		log.Printf("connect: %v", err)
		_ = srv.Process.Kill()
		return 1
	}

	epPath := filepath.Join(stateDir, endpointFile)
	ep := endpoint{URL: url, TokenFile: tokenFile, RealCLI: real, TetherPID: os.Getpid(),
		ServerPID: srv.Process.Pid, Started: time.Now().Format(time.RFC3339)}
	if b, err := json.MarshalIndent(ep, "", "  "); err == nil {
		_ = os.WriteFile(epPath, b, 0o600)
	}
	defer removeIfOurs(epPath)

	select {
	case why := <-bridge(conn):
		log.Printf("bridge closed: %s", why)
		conn.close()
		_ = srv.Process.Kill()
		<-srvDone
		return 0
	case code := <-srvDone:
		conn.close()
		return code
	}
}

// stopStaleServer stops a server whose codex-tether died without stopping
// it, identified by the process ID and executable recorded in endpoint.json.
func stopStaleServer(stateDir string) {
	epPath := filepath.Join(stateDir, endpointFile)
	b, err := os.ReadFile(epPath)
	if err != nil {
		return
	}
	var ep endpoint
	if json.Unmarshal(b, &ep) != nil || processImage(ep.TetherPID) != "" {
		return
	}
	if img := processImage(ep.ServerPID); img != "" && strings.EqualFold(filepath.Clean(img), filepath.Clean(ep.RealCLI)) {
		if p, err := os.FindProcess(ep.ServerPID); err == nil {
			log.Printf("stopping stale server pid %d", ep.ServerPID)
			_ = p.Kill()
		}
	}
	_ = os.Remove(epPath)
}

// liveEndpoint returns the published endpoint if its server answers /readyz.
func liveEndpoint(stateDir string) (*endpoint, string, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, endpointFile))
	if err != nil {
		return nil, "", errors.New("no shared server (is the Codex app running with codex-tether?)")
	}
	var ep endpoint
	if err := json.Unmarshal(b, &ep); err != nil {
		return nil, "", fmt.Errorf("bad %s: %v", endpointFile, err)
	}
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + strings.TrimPrefix(ep.URL, "ws://") + "/readyz")
	if err != nil {
		return nil, "", fmt.Errorf("shared server at %s not answering: %v", ep.URL, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("shared server at %s not ready: %s", ep.URL, resp.Status)
	}
	token, err := os.ReadFile(ep.TokenFile)
	if err != nil {
		return nil, "", err
	}
	return &ep, strings.TrimSpace(string(token)), nil
}

func printStatus(stateDir string) int {
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

func removeIfOurs(epPath string) {
	b, err := os.ReadFile(epPath)
	if err != nil {
		return
	}
	var ep endpoint
	if json.Unmarshal(b, &ep) == nil && ep.TetherPID == os.Getpid() {
		_ = os.Remove(epPath)
	}
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

func ensureToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return strings.TrimSpace(string(b)), restrictToCurrentUser(path)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, restrictToCurrentUser(path)
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
