//go:build smoke

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/proxy"
)

// TestAgainstRealServer drives the same service the window calls, against the
// server given in the environment, so the tunnel path is exercised end to end.
//
//	PT_SERVER=host PT_KEY=n go test -tags smoke -run TestAgainstRealServer -v .
func TestAgainstRealServer(t *testing.T) {
	server := os.Getenv("PT_SERVER")
	key := os.Getenv("PT_KEY")
	if server == "" {
		t.Skip("PT_SERVER not set")
	}
	port := os.Getenv("PT_PORT")
	if port == "" {
		port = "34455"
	}

	store := &Store{path: filepath.Join(t.TempDir(), "settings.json")}
	sink := setupLogging()
	sink.setFilter("debug", false)
	svc := NewTunnel(store, sink)

	s := DefaultSettings()
	s.Server = server
	s.Key = key
	s.ListenPort = port
	s.Socks5 = true
	s.TCPMode = true
	s.NoLog = false
	s.LogLevel = "info"

	status, err := svc.Start(s)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Logf("started: state=%s listen=%s server=%s", status.State, status.Listen, status.Server)
	defer func() {
		if _, err := svc.Stop(); err != nil {
			t.Logf("stop: %v", err)
		}
	}()

	// The client pings the server once a second; wait for the first answer.
	var rtt int64
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		st := svc.Status()
		if st.RTTms > 0 {
			rtt = st.RTTms
			break
		}
	}
	if rtt == 0 {
		t.Fatalf("no RTT after 20s; recent log:\n%s", recentLog(sink))
	}
	t.Logf("RTT %dms — the tunnel is up", rtt)

	// And now through it: an HTTP GET over the local socks5 port.
	dialer, err := proxy.SOCKS5("tcp", "127.0.0.1:"+port, nil, proxy.Direct)
	if err != nil {
		t.Fatalf("socks5 dialer: %v", err)
	}
	conn, err := dialer.Dial("tcp", "www.baidu.com:80")
	if err != nil {
		t.Fatalf("dial through the tunnel: %v\n%s", err, recentLog(sink))
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: www.baidu.com\r\nUser-Agent: pingtunnel-smoke\r\nConnection: close\r\n\r\n")
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read through the tunnel: %v\n%s", err, recentLog(sink))
	}
	t.Logf("HTTP over the tunnel: %s", line)
	if len(line) < 9 || line[:9] != "HTTP/1.1 " && line[:8] != "HTTP/1.0" {
		t.Fatalf("unexpected response line: %q", line)
	}

	st := svc.Status()
	t.Logf("counters: send=%d pkt recv=%d pkt conns=%d up=%ds", st.SendPkt, st.RecvPkt, st.Conn, st.UptimeSec)
}

// TestProbeServer checks the raw-socket probe the "test server" button runs:
// it tells whether this machine can receive ICMP from the server at all.
func TestProbeServer(t *testing.T) {
	server := os.Getenv("PT_SERVER")
	if server == "" {
		t.Skip("PT_SERVER not set")
	}
	svc := NewTunnel(&Store{path: filepath.Join(t.TempDir(), "settings.json")}, newLogSink())
	res, err := svc.Probe(server)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	t.Logf("probe %s (%s): alive=%v %d/%d min=%.1f avg=%.1f max=%.1f — %s",
		res.Host, res.IP, res.Alive, res.Recv, res.Sent, res.MinMs, res.AvgMs, res.MaxMs, res.Message)
	if !res.Alive {
		t.Fatalf("no ICMP reply from the server")
	}
}

func recentLog(sink *logSink) string {	lines := sink.snapshot()
	out := ""
	start := len(lines) - 15
	if start < 0 {
		start = 0
	}
	for _, l := range lines[start:] {
		out += l.Time + " " + l.Level + " " + l.Text + "\n"
	}
	return out
}
