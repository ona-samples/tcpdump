package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

const (
	webSocketTimeout    = 15 * time.Second
	webSocketPingPeriod = (webSocketTimeout * 9) / 10
	webSocketWriteWait  = 10 * time.Second

	webSocketTunnelHeader = "X-Gitpod-WebSocket-Tunnel"
	webSocketTunnelValue  = "ssh"
	sshBridgeHeader       = "X-Gitpod-Bridge"
	sshBridgeValue        = "SSH"
)

type webSocketConn struct {
	*websocket.Conn

	reader io.Reader
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
}

func newWebSocketConn(ctx context.Context, ws *websocket.Conn) *webSocketConn {
	ctx, cancel := context.WithCancel(ctx)
	c := &webSocketConn{
		Conn:   ws,
		ctx:    ctx,
		cancel: cancel,
	}

	_ = c.Conn.SetReadDeadline(time.Now().Add(webSocketTimeout))
	c.SetPingHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(webSocketTimeout))
		return c.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(webSocketWriteWait))
	})
	c.SetPongHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(webSocketTimeout))
		return nil
	})

	go c.pingRoutine()
	return c
}

func (c *webSocketConn) pingRoutine() {
	ticker := time.NewTicker(webSocketPingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if err := c.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(webSocketWriteWait)); err != nil {
				_ = c.Close()
				return
			}
		}
	}
}

func (c *webSocketConn) Read(dst []byte) (int, error) {
	for {
		if c.reader != nil {
			n, err := c.reader.Read(dst)
			if n > 0 {
				_ = c.Conn.SetReadDeadline(time.Now().Add(webSocketTimeout))
				return n, nil
			}
			if errors.Is(err, io.EOF) {
				c.reader = nil
				continue
			}
			return 0, err
		}

		_, reader, err := c.Conn.NextReader()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				return 0, io.EOF
			}
			return 0, err
		}
		c.reader = reader
	}
}

func (c *webSocketConn) Write(b []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(webSocketWriteWait))
	w, err := c.Conn.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return 0, err
	}
	n, err := w.Write(b)
	if err != nil {
		_ = w.Close()
		return n, err
	}
	return n, w.Close()
}

func (c *webSocketConn) Close() error {
	c.once.Do(func() {
		c.cancel()
		_ = c.Conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(webSocketWriteWait),
		)
		_ = c.Conn.Close()
	})
	return nil
}

func (c *webSocketConn) LocalAddr() net.Addr  { return c.Conn.LocalAddr() }
func (c *webSocketConn) RemoteAddr() net.Addr { return c.Conn.RemoteAddr() }
func (c *webSocketConn) SetDeadline(t time.Time) error {
	return errors.Join(c.SetReadDeadline(t), c.SetWriteDeadline(t))
}
func (c *webSocketConn) SetReadDeadline(t time.Time) error { return c.Conn.SetReadDeadline(t) }
func (c *webSocketConn) SetWriteDeadline(t time.Time) error {
	return c.Conn.SetWriteDeadline(t)
}

type server struct {
	mode       string
	addr       string
	sshTarget  string
	httpServer *http.Server
}

func main() {
	var s server
	flag.StringVar(&s.mode, "mode", "http", "service mode: http, websocket, or ssh")
	flag.StringVar(&s.addr, "addr", ":8080", "listen address")
	flag.StringVar(&s.sshTarget, "ssh-target", defaultSSHTarget(), "target SSH address for ssh mode")
	flag.Parse()

	switch s.mode {
	case "http", "websocket", "ssh":
	default:
		log.Fatalf("invalid mode %q", s.mode)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := s.run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func defaultSSHTarget() string {
	for _, key := range []string{"ONA_SSH_TARGET_ADDR", "SSH_TARGET_ADDR", "TARGET_SSH_ADDR"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return "127.0.0.1:22222"
}

func (s *server) run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/_health", s.handleHealth)

	switch s.mode {
	case "http":
		mux.HandleFunc("/", s.handleHTTPTimeStream)
	case "websocket":
		mux.HandleFunc("/", s.handleWebSocketTimeStream)
	case "ssh":
		mux.HandleFunc("/", s.handleSSH)
	}

	s.httpServer = &http.Server{
		Addr:              s.addr,
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("starting %s troubleshooting service on %s", s.mode, s.addr)
	if s.mode == "ssh" {
		log.Printf("forwarding WebSocket SSH tunnels to %s", s.sshTarget)
	}
	return s.httpServer.ListenAndServe()
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if s.mode == "ssh" {
		conn, err := net.DialTimeout("tcp", s.sshTarget, time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		_ = conn.Close()
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *server) handleHTTPTimeStream(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Troubleshoot-Protocol", r.Proto)
	w.WriteHeader(http.StatusOK)

	flusher, _ := w.(http.Flusher)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		if _, err := fmt.Fprintf(w, "%s %s\n", time.Now().UTC().Format(time.RFC3339Nano), r.Proto); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *server) handleWebSocketTimeStream(w http.ResponseWriter, r *http.Request) {
	if !websocket.IsWebSocketUpgrade(r) {
		http.Error(w, "websocket upgrade required", http.StatusUpgradeRequired)
		return
	}

	upgrader := websocket.Upgrader{
		EnableCompression: false,
		CheckOrigin:       func(*http.Request) bool { return true },
		ReadBufferSize:    32 * 1024,
		WriteBufferSize:   32 * 1024,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(webSocketWriteWait)); err != nil {
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(time.Now().UTC().Format(time.RFC3339Nano))); err != nil {
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *server) handleSSH(w http.ResponseWriter, r *http.Request) {
	if isSSHBridgeRequest(r) {
		s.handleSSHConnect(w, r)
		return
	}
	if !isWebSocketTunnelRequest(r) {
		http.Error(w, "websocket tunnel required", http.StatusUpgradeRequired)
		return
	}
	s.handleWebSocketSSHTunnel(w, r)
}

func isSSHBridgeRequest(r *http.Request) bool {
	return r.Method == http.MethodConnect && strings.EqualFold(r.Header.Get(sshBridgeHeader), sshBridgeValue)
}

func isWebSocketTunnelRequest(r *http.Request) bool {
	if !websocket.IsWebSocketUpgrade(r) {
		return false
	}
	if r.Header.Get(webSocketTunnelHeader) == webSocketTunnelValue {
		return true
	}
	for _, protocol := range websocket.Subprotocols(r) {
		if protocol == webSocketTunnelValue {
			return true
		}
	}
	return false
}

func (s *server) handleWebSocketSSHTunnel(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		EnableCompression: false,
		CheckOrigin:       func(*http.Request) bool { return true },
		Subprotocols:      []string{webSocketTunnelValue},
		ReadBufferSize:    32 * 1024,
		WriteBufferSize:   32 * 1024,
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ssh websocket upgrade failed: %v", err)
		return
	}

	wsConn := newWebSocketConn(r.Context(), conn)
	defer wsConn.Close()

	targetConn, err := (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(r.Context(), "tcp", s.sshTarget)
	if err != nil {
		log.Printf("ssh target dial failed: %v", err)
		return
	}
	defer targetConn.Close()

	start := time.Now()
	log.Printf("ssh websocket tunnel opened target=%s remote=%s", s.sshTarget, r.RemoteAddr)
	err = bidirectionalCopy(r.Context(), targetConn, wsConn)
	log.Printf("ssh websocket tunnel closed duration=%s err=%v", time.Since(start).Round(time.Millisecond), err)
}

func (s *server) handleSSHConnect(w http.ResponseWriter, r *http.Request) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	targetConn, err := (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext(r.Context(), "tcp", s.sshTarget)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer targetConn.Close()

	clientConn, bufrw, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer clientConn.Close()

	if _, err := bufrw.WriteString("HTTP/1.1 200 OK\r\n\r\n"); err != nil {
		return
	}
	if err := bufrw.Flush(); err != nil {
		return
	}

	start := time.Now()
	log.Printf("ssh connect tunnel opened target=%s remote=%s", s.sshTarget, r.RemoteAddr)
	err = bidirectionalCopy(r.Context(), targetConn, clientConn)
	log.Printf("ssh connect tunnel closed duration=%s err=%v", time.Since(start).Round(time.Millisecond), err)
}

func bidirectionalCopy(ctx context.Context, targetConn net.Conn, clientConn net.Conn) error {
	copyCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errs := make(chan error, 3)
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = clientConn.Close()
			_ = targetConn.Close()
		})
	}

	go func() {
		<-copyCtx.Done()
		closeBoth()
		errs <- copyCtx.Err()
	}()

	var bytesClientToTarget atomic.Int64
	var bytesTargetToClient atomic.Int64

	go func() {
		defer cancel()
		buf := make([]byte, 32*1024)
		n, err := io.CopyBuffer(targetConn, clientConn, buf)
		bytesClientToTarget.Add(n)
		errs <- err
	}()
	go func() {
		defer cancel()
		buf := make([]byte, 32*1024)
		n, err := io.CopyBuffer(clientConn, targetConn, buf)
		bytesTargetToClient.Add(n)
		errs <- err
	}()

	err := <-errs
	closeBoth()
	log.Printf("ssh tunnel bytes client_to_target=%d target_to_client=%d", bytesClientToTarget.Load(), bytesTargetToClient.Load())
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
