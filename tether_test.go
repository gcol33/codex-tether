package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
)

func TestIsStdioAppServer(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"app-server"}, true},
		{[]string{"-c", "features.code_mode_host=true", "app-server", "--analytics-default-enabled",
			"-c", "features.prefer_mxc=true"}, true},
		{[]string{"--enable", "x", "app-server", "--disable", "y"}, true},
		{[]string{"app-server", "--listen", "ws://127.0.0.1:1"}, false},
		{[]string{"app-server", "--listen=unix://"}, false},
		{[]string{"app-server", "--stdio"}, false},
		{[]string{"app-server", "daemon", "version"}, false},
		{[]string{"app-server", "proxy"}, false},
		{[]string{"-c", "forced_login_method=\"chatgpt\"", "exec-server", "--remote", "https://x"}, false},
		{[]string{"resume", "--last"}, false},
		{[]string{"-c", "app-server"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := isStdioAppServer(c.args); got != c.want {
			t.Errorf("isStdioAppServer(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestPackageFamilyFromPath(t *testing.T) {
	cases := map[string]string{
		`C:\Program Files\WindowsApps\OpenAI.Codex_26.1013.4894.0_x64__2p2nqsd0c76g0\app\ChatGPT.exe`: "OpenAI.Codex_2p2nqsd0c76g0",
		`C:/Program Files/WindowsApps/Foo.Bar_1.2.3.0_arm64__abc123/x.exe`:                             "Foo.Bar_abc123",
		`C:\Program Files\Git\usr\bin\bash.exe`:                                                         "",
		`C:\Program Files\WindowsApps`:                                                                  "",
		`C:\Program Files\WindowsApps\NoPublisher_1.0\x.exe`:                                            "",
		"": "",
	}
	for in, want := range cases {
		if got := packageFamilyFromPath(in); got != want {
			t.Errorf("packageFamilyFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChildEnv(t *testing.T) {
	app := `C:\Program Files\WindowsApps\OpenAI.Codex_26.1013.4894.0_x64__2p2nqsd0c76g0\app\ChatGPT.exe`
	got := childEnv([]string{"A=1"}, app)
	if want := []string{"A=1", sandboxPackageFamilyVar + "=OpenAI.Codex_2p2nqsd0c76g0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("packaged parent: got %q, want %q", got, want)
	}
	keep := []string{sandboxPackageFamilyVar + "=Other_x"}
	if got := childEnv(keep, app); !reflect.DeepEqual(got, keep) {
		t.Errorf("existing value overwritten: %q", got)
	}
	if got := childEnv([]string{"A=1"}, `C:\bash.exe`); !reflect.DeepEqual(got, []string{"A=1"}) {
		t.Errorf("unpackaged parent: %q", got)
	}
}

func TestWithRemote(t *testing.T) {
	r := []string{"--remote", "ws://h:1", "--remote-auth-token-env", tokenEnvVar}
	if got, want := withRemote([]string{"resume", "--last"}, "ws://h:1"), append([]string{"resume", "--last"}, r...); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	got := withRemote([]string{"resume", "--", "--not-a-flag"}, "ws://h:1")
	want := append(append([]string{"resume"}, r...), "--", "--not-a-flag")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAcceptKey(t *testing.T) {
	// RFC 6455 section 1.3 example.
	if got := acceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("acceptKey = %q", got)
	}
}

func serverFrame(op byte, fin bool, payload []byte) []byte {
	b := appendFrameHeader(nil, op, len(payload), nil)
	if !fin {
		b[0] &^= 0x80
	}
	return append(b, payload...)
}

func TestFrameRoundTrip(t *testing.T) {
	for _, n := range []int{0, 5, 125, 126, 65535, 65536, 200000} {
		client, server := net.Pipe()
		w := &wsConn{c: client, r: bufio.NewReader(client)}
		payload := bytes.Repeat([]byte("x"), n)
		go func() { _ = w.writeText(payload) }()
		fin, op, got, err := readFrame(server)
		if err != nil || !fin || op != opText || !bytes.Equal(got, payload) {
			t.Errorf("n=%d: fin=%v op=%d len=%d err=%v", n, fin, op, len(got), err)
		}
		client.Close()
		server.Close()
	}
}

func TestReadMessageFragmentsAndPing(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	w := &wsConn{c: client, r: bufio.NewReader(client)}

	go func() {
		server.Write(serverFrame(opText, false, []byte(`{"a":`)))
		server.Write(serverFrame(opPing, true, []byte("p")))
		fin, op, payload, err := readFrame(server) // pong from the client
		if err != nil || !fin || op != opPong || string(payload) != "p" {
			server.Write(serverFrame(opText, true, []byte("bad pong")))
			return
		}
		server.Write(serverFrame(opContinuation, true, []byte(`1}`)))
		server.Write(serverFrame(opClose, true, nil))
		readFrame(server) // close echo
	}()

	msg, err := w.readMessage()
	if err != nil || string(msg) != `{"a":1}` {
		t.Fatalf("readMessage = %q, %v", msg, err)
	}
	if _, err := w.readMessage(); err != io.EOF {
		t.Fatalf("after close: %v, want EOF", err)
	}
}

func TestHandshake(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	go func() {
		r := bufio.NewReader(server)
		var key, auth string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if k, v, ok := strings.Cut(line, ": "); ok {
				switch k {
				case "Sec-WebSocket-Key":
					key = v
				case "Authorization":
					auth = v
				}
			}
		}
		if auth != "Bearer secret" {
			io.WriteString(server, "HTTP/1.1 401 Unauthorized\r\nContent-Length: 0\r\n\r\n")
			return
		}
		io.WriteString(server, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: "+acceptKey(key)+"\r\n\r\n")
	}()
	if _, err := handshake(client, "h", "secret"); err != nil {
		t.Fatalf("handshake: %v", err)
	}
}
