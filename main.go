// codex-share stands in for the Codex desktop app's CLI binary (CODEX_CLI_PATH).
//
// When the app starts its stdio app-server, codex-share starts the real
// app-server on a token-protected loopback WebSocket instead and bridges the
// app's stdio to it, so other clients (the TUI over SSH via `--remote`) attach
// to the same server and share its threads instead of fighting over the
// per-thread writer lock. Every other invocation runs the real binary
// unchanged.
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
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
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	sandboxPackageFamilyVar = "CODEX_WINDOWS_SANDBOX_PACKAGE_FAMILY"
	sandboxPackageFamily    = "OpenAI.Codex_2p2nqsd0c76g0"
	disableVar              = "CODEX_SHARE_DISABLE"
	wsGUID                  = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

type endpoint struct {
	URL       string `json:"url"`
	TokenFile string `json:"token_file"`
	RealCLI   string `json:"real_cli"`
	ShimPID   int    `json:"shim_pid"`
	ServerPID int    `json:"server_pid"`
	Started   string `json:"started"`
}

func main() {
	stateDir := filepath.Join(os.Getenv("USERPROFILE"), ".codex", "shared-server")
	_ = os.MkdirAll(stateDir, 0o700)
	if f, err := os.OpenFile(filepath.Join(stateDir, "shim.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		log.SetOutput(f)
	} else {
		log.SetOutput(io.Discard)
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// Children die with the shim: when the app terminates "its CLI", the real
	// process tree behind it goes too, and no orphan keeps holding thread locks.
	if err := killChildrenOnExit(); err != nil {
		log.Printf("job object: %v", err)
	}

	if len(os.Args) == 2 && os.Args[1] == "--attach" {
		os.Exit(attach(stateDir))
	}

	real, err := realCLI()
	if err != nil {
		log.Printf("resolve real cli: %v", err)
		fmt.Fprintln(os.Stderr, "codex-share:", err)
		os.Exit(1)
	}
	args := os.Args[1:]
	env := childEnv()

	if os.Getenv(disableVar) == "1" || !isStdioAppServer(args) {
		log.Printf("passthrough: %q", args)
		os.Exit(passthrough(real, args, env))
	}
	log.Printf("shared app-server: %q (real %s)", args, real)
	os.Exit(serveShared(real, args, env, stateDir))
}

// realCLI returns the newest codex.exe the desktop app has staged under
// %LOCALAPPDATA%\OpenAI\Codex\bin\<hash>\.
func realCLI() (string, error) {
	if p := strings.TrimSpace(os.Getenv("CODEX_SHARE_REAL_CLI")); p != "" {
		return p, nil
	}
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenAI", "Codex", "bin", "*", "codex.exe"))
	if len(matches) == 0 {
		return "", errors.New("no staged codex.exe under %LOCALAPPDATA%\\OpenAI\\Codex\\bin")
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

// childEnv restores what the app strips when CODEX_CLI_PATH is overridden.
func childEnv() []string {
	env := os.Environ()
	if os.Getenv(sandboxPackageFamilyVar) == "" {
		env = append(env, sandboxPackageFamilyVar+"="+sandboxPackageFamily)
	}
	return env
}

// valueFlags take a separate value argument on `codex [global] app-server`.
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
		log.Printf("passthrough run: %v", err)
		fmt.Fprintln(os.Stderr, "codex-share:", err)
		return 1
	}
	return 0
}

func serveShared(real string, args, env []string, stateDir string) int {
	tokenFile := filepath.Join(stateDir, "token")
	token, err := ensureToken(tokenFile)
	if err != nil {
		log.Printf("token: %v", err)
		return passthrough(real, args, env)
	}
	port, err := freePort()
	if err != nil {
		log.Printf("port: %v", err)
		return passthrough(real, args, env)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	url := "ws://" + addr

	srvArgs := append(append([]string{}, args...),
		"--listen", url, "--ws-auth", "capability-token", "--ws-token-file", tokenFile)
	srv := exec.Command(real, srvArgs...)
	srv.Env = env
	srv.Stdout = os.Stderr
	srv.Stderr = os.Stderr
	if err := srv.Start(); err != nil {
		log.Printf("start server: %v", err)
		return passthrough(real, args, env)
	}
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

	epPath := filepath.Join(stateDir, "endpoint.json")
	ep := endpoint{URL: url, TokenFile: tokenFile, RealCLI: real, ShimPID: os.Getpid(),
		ServerPID: srv.Process.Pid, Started: time.Now().Format(time.RFC3339)}
	if b, err := json.MarshalIndent(ep, "", "  "); err == nil {
		_ = os.WriteFile(epPath, b, 0o600)
	}
	defer removeIfOurs(epPath)

	bridgeDone := bridge(conn)
	select {
	case why := <-bridgeDone:
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

// attach bridges stdio to the shared server published in endpoint.json.
func attach(stateDir string) int {
	b, err := os.ReadFile(filepath.Join(stateDir, "endpoint.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-share: no shared server running")
		return 1
	}
	var ep endpoint
	if err := json.Unmarshal(b, &ep); err != nil {
		fmt.Fprintln(os.Stderr, "codex-share: bad endpoint.json:", err)
		return 1
	}
	token, err := os.ReadFile(ep.TokenFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-share:", err)
		return 1
	}
	conn, err := dialWS(strings.TrimPrefix(ep.URL, "ws://"), strings.TrimSpace(string(token)))
	if err != nil {
		fmt.Fprintln(os.Stderr, "codex-share:", err)
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
	bridgeDone := make(chan string, 2)
	go func() { // stdin -> server
		r := bufio.NewReaderSize(os.Stdin, 1<<20)
		for {
			line, err := r.ReadBytes('\n')
			line = trimEOL(line)
			if len(line) > 0 {
				if werr := conn.writeText(line); werr != nil {
					bridgeDone <- "write: " + werr.Error()
					return
				}
			}
			if err != nil {
				bridgeDone <- "stdin: " + err.Error()
				return
			}
		}
	}()
	go func() { // server -> stdout
		w := bufio.NewWriterSize(os.Stdout, 1<<20)
		for {
			msg, err := conn.readMessage()
			if err != nil {
				bridgeDone <- "read: " + err.Error()
				return
			}
			w.Write(msg)
			w.WriteByte('\n')
			if err := w.Flush(); err != nil {
				bridgeDone <- "stdout: " + err.Error()
				return
			}
		}
	}()
	return bridgeDone
}

func removeIfOurs(epPath string) {
	b, err := os.ReadFile(epPath)
	if err != nil {
		return
	}
	var ep endpoint
	if json.Unmarshal(b, &ep) == nil && ep.ShimPID == os.Getpid() {
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
		return strings.TrimSpace(string(b)), nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
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

// Minimal RFC 6455 client: text messages, fragmentation, ping/pong, close.

type wsConn struct {
	c  net.Conn
	r  *bufio.Reader
	mu sync.Mutex
}

func dialWS(addr, token string) (*wsConn, error) {
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	keyRaw := make([]byte, 16)
	_, _ = rand.Read(keyRaw)
	key := base64.StdEncoding.EncodeToString(keyRaw)
	req := "GET / HTTP/1.1\r\n" +
		"Host: " + addr + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Authorization: Bearer " + token + "\r\n\r\n"
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(c, req); err != nil {
		c.Close()
		return nil, err
	}
	r := bufio.NewReaderSize(c, 1<<20)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		c.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		c.Close()
		return nil, fmt.Errorf("handshake status %s", resp.Status)
	}
	sum := sha1.Sum([]byte(key + wsGUID))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		c.Close()
		return nil, errors.New("bad Sec-WebSocket-Accept")
	}
	_ = c.SetDeadline(time.Time{})
	return &wsConn{c: c, r: r}, nil
}

func (w *wsConn) writeFrame(op byte, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|op)
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	hdr = append(hdr, mask...)
	body := make([]byte, n)
	for i := range payload {
		body[i] = payload[i] ^ mask[i&3]
	}
	if _, err := w.c.Write(hdr); err != nil {
		return err
	}
	_, err := w.c.Write(body)
	return err
}

func (w *wsConn) writeText(p []byte) error { return w.writeFrame(0x1, p) }

func (w *wsConn) readMessage() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0F
		masked := h[1]&0x80 != 0
		n := uint64(h[1] & 0x7F)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.r, mask[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(w.r, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i&3]
			}
		}
		switch op {
		case 0x8:
			_ = w.writeFrame(0x8, nil)
			return nil, io.EOF
		case 0x9:
			if err := w.writeFrame(0xA, payload); err != nil {
				return nil, err
			}
			continue
		case 0xA:
			continue
		}
		msg = append(msg, payload...)
		if fin {
			return msg, nil
		}
	}
}

func (w *wsConn) close() {
	_ = w.writeFrame(0x8, []byte{0x03, 0xE8})
	_ = w.c.Close()
}

// Job object with KILL_ON_JOB_CLOSE; children inherit it and may break away
// explicitly (the daemon launcher does).

type ioCounters struct{ a, b, c, d, e, f uint64 }

type basicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type extendedLimit struct {
	Basic                 basicLimit
	Io                    ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

func killChildrenOnExit() error {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	create := k32.NewProc("CreateJobObjectW")
	setInfo := k32.NewProc("SetInformationJobObject")
	assign := k32.NewProc("AssignProcessToJobObject")
	getCur := k32.NewProc("GetCurrentProcess")

	job, _, err := create.Call(0, 0)
	if job == 0 {
		return err
	}
	var info extendedLimit
	info.Basic.LimitFlags = 0x2000 | 0x800 // KILL_ON_JOB_CLOSE | BREAKAWAY_OK
	if r, _, err := setInfo.Call(job, 9, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); r == 0 {
		return err
	}
	self, _, _ := getCur.Call()
	if r, _, err := assign.Call(job, self); r == 0 {
		return err
	}
	return nil
}
