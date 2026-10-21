package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

// windowsProxyBridge is an HTTP proxy in front of the tunnel's socks5 port.
// Windows' system proxy setting cannot speak SOCKS, so bringing the tunnel to
// "global" on Windows means handing WinINET apps an HTTP proxy that forwards
// into socks5.
type windowsProxyBridge struct {
	ln   net.Listener
	dial proxy.Dialer
	stop chan struct{}
	addr string
}

func startBridge(listenAddr, socksAddr string) (*windowsProxyBridge, error) {
	d, err := proxy.SOCKS5("tcp", socksAddr, nil, &net.Dialer{Timeout: 20 * time.Second})
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}
	b := &windowsProxyBridge{ln: ln, dial: d, stop: make(chan struct{}), addr: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go b.serve(conn)
		}
	}()
	return b, nil
}

func (b *windowsProxyBridge) Addr() string { return b.addr }

func (b *windowsProxyBridge) Close() {
	select {
	case <-b.stop:
	default:
		close(b.stop)
	}
	_ = b.ln.Close()
}

func (b *windowsProxyBridge) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	br := bufio.NewReader(conn)
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		if req.Method == http.MethodConnect {
			b.tunnel(conn, br, req.Host, "443")
			return
		}
		if !b.forward(conn, req) {
			return
		}
		if req.Close {
			return
		}
	}
}

// tunnel answers a CONNECT and then copies bytes both ways through socks5.
func (b *windowsProxyBridge) tunnel(conn net.Conn, br *bufio.Reader, host, defaultPort string) {
	target := withPort(host, defaultPort)
	upstream, err := b.dial.Dial("tcp", target)
	if err != nil {
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 502 Bad Gateway\r\nProxy-Agent: pingtunnel-workbench\r\n\r\n")
		return
	}
	defer upstream.Close()
	if _, err := fmt.Fprintf(conn, "HTTP/1.1 200 Connection Established\r\nProxy-Agent: pingtunnel-workbench\r\n\r\n"); err != nil {
		return
	}
	go func() { _, _ = io.Copy(upstream, br) }()
	_, _ = io.Copy(conn, upstream)
}

// forward sends one plain HTTP request through the tunnel and streams the
// answer back: GET http://host/path arrives with an absolute URI.
func (b *windowsProxyBridge) forward(conn net.Conn, req *http.Request) bool {
	if req.URL == nil || req.URL.Host == "" {
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 400 Bad Request\r\n\r\n")
		return false
	}
	target := withPort(req.URL.Host, "80")
	upstream, err := b.dial.Dial("tcp", target)
	if err != nil {
		_, _ = fmt.Fprintf(conn, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return false
	}
	defer upstream.Close()
	req.RequestURI = ""
	req.Header.Del("Proxy-Connection")
	if err := req.Write(upstream); err != nil {
		return false
	}
	_, _ = io.Copy(conn, upstream)
	return false // one request per connection keeps this simple and correct
}

func withPort(host, port string) string {
	if strings.Contains(host, ":") {
		return host
	}
	return net.JoinHostPort(host, port)
}

// socksDialer is the plain socks5 client used by the readiness probes.
func socksDialer(addr string) (proxy.Dialer, error) {
	return proxy.SOCKS5("tcp", addr, nil, &net.Dialer{Timeout: 8 * time.Second})
}

// probeDataPath proves the tunnel moves real bytes, not just pings: a socks5
// CONNECT that completes is the only signal worth trusting before handing the
// machine's traffic to it.
func probeDataPath(socksAddr, target string, timeout time.Duration) error {
	if socksAddr == "" {
		return fmt.Errorf("没有 socks5 端口")
	}
	d, err := socksDialer(socksAddr)
	if err != nil {
		return err
	}
	conn, err := d.Dial("tcp", target)
	if err != nil {
		return fmt.Errorf("通过隧道连接 %s 失败: %w", target, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	return nil
}
