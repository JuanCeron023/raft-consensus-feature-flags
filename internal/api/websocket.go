package api

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"strings"
)

// handleWebSocket upgrades the connection using a minimal RFC 6455 handshake
// and streams status snapshots as text frames. Implementing the framing by
// hand (instead of pulling gorilla/websocket) keeps the binary dependency-free
// and demonstrates understanding of the protocol.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") ||
		!strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "expected websocket upgrade", http.StatusBadRequest)
		return
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "missing Sec-WebSocket-Key", http.StatusBadRequest)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()

	accept := computeAcceptKey(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
	if _, err := buf.WriteString(resp); err != nil {
		return
	}
	if err := buf.Flush(); err != nil {
		return
	}

	ch := make(chan []byte, 16)
	s.mu.Lock()
	s.watchers[ch] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.watchers, ch)
		s.mu.Unlock()
	}()

	// Reader goroutine: detect client close/ping so we can tear down.
	go drainClient(buf)

	for payload := range ch {
		if err := writeTextFrame(buf, payload); err != nil {
			return
		}
	}
}

const wsMagic = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

func computeAcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key + wsMagic))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// writeTextFrame writes an unmasked server->client text frame.
func writeTextFrame(buf *bufio.ReadWriter, payload []byte) error {
	var header []byte
	fin := byte(0x80)
	opcode := byte(0x1) // text
	header = append(header, fin|opcode)

	n := len(payload)
	switch {
	case n <= 125:
		header = append(header, byte(n))
	case n <= 0xFFFF:
		header = append(header, 126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		header = append(header, ext[:]...)
	default:
		header = append(header, 127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(header, ext[:]...)
	}
	if _, err := buf.Write(header); err != nil {
		return err
	}
	if _, err := buf.Write(payload); err != nil {
		return err
	}
	return buf.Flush()
}

// drainClient reads and discards incoming frames until the connection closes,
// so the write loop can observe a broken pipe promptly.
func drainClient(buf *bufio.ReadWriter) {
	tmp := make([]byte, 512)
	for {
		if _, err := buf.Read(tmp); err != nil {
			return
		}
	}
}
