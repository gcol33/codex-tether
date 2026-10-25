package main

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

// Minimal RFC 6455 client: text messages, fragmentation, ping/pong, close.

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// maxFrameSize bounds the payload a frame header may announce.
const maxFrameSize = 1 << 30

const (
	opContinuation = 0x0
	opText         = 0x1
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

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
	w, err := handshake(c, addr, token)
	if err != nil {
		c.Close()
		return nil, err
	}
	return w, nil
}

func handshake(c net.Conn, host, token string) (*wsConn, error) {
	keyRaw := make([]byte, 16)
	_, _ = rand.Read(keyRaw)
	key := base64.StdEncoding.EncodeToString(keyRaw)
	req := "GET / HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Authorization: Bearer " + token + "\r\n\r\n"
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(c, req); err != nil {
		return nil, err
	}
	r := bufio.NewReaderSize(c, 1<<20)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("handshake status %s", resp.Status)
	}
	if resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(key) {
		return nil, errors.New("bad Sec-WebSocket-Accept")
	}
	_ = c.SetDeadline(time.Time{})
	return &wsConn{c: c, r: r}, nil
}

func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

// appendFrameHeader appends FIN|op and the payload length; mask is nil for
// unmasked (server-to-client) frames.
func appendFrameHeader(hdr []byte, op byte, n int, mask []byte) []byte {
	maskBit := byte(0)
	if mask != nil {
		maskBit = 0x80
	}
	hdr = append(hdr, 0x80|op)
	switch {
	case n < 126:
		hdr = append(hdr, maskBit|byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, maskBit|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, maskBit|127)
		hdr = binary.BigEndian.AppendUint64(hdr, uint64(n))
	}
	return append(hdr, mask...)
}

func (w *wsConn) writeFrame(op byte, payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	frame := appendFrameHeader(make([]byte, 0, 14+len(payload)), op, len(payload), mask)
	for i, b := range payload {
		frame = append(frame, b^mask[i&3])
	}
	_, err := w.c.Write(frame)
	return err
}

func (w *wsConn) writeText(p []byte) error { return w.writeFrame(opText, p) }

// readMessage returns the next complete data message, answering pings and
// reassembling fragments.
func (w *wsConn) readMessage() ([]byte, error) {
	var msg []byte
	for {
		fin, op, payload, err := readFrame(w.r)
		if err != nil {
			return nil, err
		}
		switch op {
		case opClose:
			_ = w.writeFrame(opClose, nil)
			return nil, io.EOF
		case opPing:
			if err := w.writeFrame(opPong, payload); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		}
		msg = append(msg, payload...)
		if fin {
			return msg, nil
		}
	}
}

func readFrame(r io.Reader) (fin bool, op byte, payload []byte, err error) {
	var h [2]byte
	if _, err = io.ReadFull(r, h[:]); err != nil {
		return
	}
	fin, op = h[0]&0x80 != 0, h[0]&0x0F
	masked := h[1]&0x80 != 0
	n := uint64(h[1] & 0x7F)
	switch n {
	case 126:
		var b [2]byte
		if _, err = io.ReadFull(r, b[:]); err != nil {
			return
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
	case 127:
		var b [8]byte
		if _, err = io.ReadFull(r, b[:]); err != nil {
			return
		}
		n = binary.BigEndian.Uint64(b[:])
	}
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(r, mask[:]); err != nil {
			return
		}
	}
	if n > maxFrameSize {
		err = fmt.Errorf("frame of %d bytes exceeds %d", n, maxFrameSize)
		return
	}
	payload = make([]byte, n)
	if _, err = io.ReadFull(r, payload); err != nil {
		return
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return
}

func (w *wsConn) close() {
	_ = w.writeFrame(opClose, []byte{0x03, 0xE8})
	_ = w.c.Close()
}
