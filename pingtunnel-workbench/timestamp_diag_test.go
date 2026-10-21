//go:build diag

package main

import (
	"encoding/binary"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// TestTimestamp asks a different question than TestWire: does this path carry
// an ICMP reply whose bytes differ from the request? A middlebox that only lets
// a reply through when it matches the request byte for byte drops type 14 while
// passing the kernel's echo — which is exactly what a silent pingtunnel looks
// like from here.
//
//	PT_SERVER=host go test -tags diag -run TestTimestamp -v .
func TestTimestamp(t *testing.T) {
	host := os.Getenv("PT_SERVER")
	if host == "" {
		t.Skip("PT_SERVER not set")
	}
	dst := &net.IPAddr{IP: net.ParseIP(host)}
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer conn.Close()

	for seq := 0; seq < 3; seq++ {
		b := make([]byte, 20)
		b[0] = byte(ipv4.ICMPTypeTimestamp) // 13, timestamp request
		b[1] = 0
		binary.BigEndian.PutUint16(b[4:], 0x4d2)
		binary.BigEndian.PutUint16(b[6:], uint16(seq))
		cs := icmpChecksum(b)
		b[2], b[3] = byte(cs>>8), byte(cs)

		start := time.Now()
		if _, err := conn.WriteTo(b, dst); err != nil {
			t.Fatalf("write: %v", err)
		}
		deadline := time.Now().Add(1500 * time.Millisecond)
		got := false
		buf := make([]byte, 1500)
		for time.Now().Before(deadline) {
			_ = conn.SetReadDeadline(deadline)
			n, src, err := conn.ReadFrom(buf)
			if err != nil {
				break
			}
			if buf[0] != byte(ipv4.ICMPTypeTimestampReply) {
				continue
			}
			got = true
			t.Logf("seq=%d: type14 reply from %s len=%d rtt=%s", seq, src, n, time.Since(start).Round(time.Millisecond))
			break
		}
		if !got {
			t.Logf("seq=%d: no type14 reply", seq)
		}
	}
}

func icmpChecksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i+1 < len(b); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(b[i:]))
	}
	if len(b)%2 == 1 {
		sum += uint32(b[len(b)-1]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
