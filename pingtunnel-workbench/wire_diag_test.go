//go:build diag

package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/esrrhs/pingtunnel"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"google.golang.org/protobuf/proto"
)

// TestWire separates two things that look alike on the wire: the echo the
// server's own kernel sends back (payload untouched, rproto 0) and the answer
// pingtunnel itself writes (rproto -1). A packet with a broken magic is only
// echoed by the kernel, never answered by pingtunnel.
//
//	PT_SERVER=host PT_KEY=n go test -tags diag -run TestWire -v .
func TestWire(t *testing.T) {
	host := os.Getenv("PT_SERVER")
	if host == "" {
		t.Skip("PT_SERVER not set")
	}
	key, _ := strconv.Atoi(os.Getenv("PT_KEY"))
	dst := &net.IPAddr{IP: net.ParseIP(host)}

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer conn.Close()

	const id = 0x4d2
	seq := 0

	send := func(label string, msg *pingtunnel.MyMsg, wait time.Duration) {
		seq++
		payload, err := proto.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		packet, err := (&icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{ID: id, Seq: seq, Data: payload},
		}).Marshal(nil)
		if err != nil {
			t.Fatalf("icmp marshal: %v", err)
		}
		start := time.Now()
		if _, err := conn.WriteTo(packet, dst); err != nil {
			t.Fatalf("write: %v", err)
		}
		t.Logf("→ %s: seq=%d sent %d bytes", label, seq, len(packet))

		deadline := start.Add(wait)
		for time.Now().Before(deadline) {
			_ = conn.SetReadDeadline(deadline)
			buf := make([]byte, 2048)
			n, src, err := conn.ReadFrom(buf)
			if err != nil {
				break
			}
			m := &pingtunnel.MyMsg{}
			_ = proto.Unmarshal(buf[8:n], m)
			t.Logf("   ← from %s icmpType=%d id=%d seq=%d len=%d type=%d key=%d magic=%d rproto=%d (rtt %s)",
				src, buf[0], int(buf[4])<<8|int(buf[5]), int(buf[6])<<8|int(buf[7]), n,
				m.Type, m.Key, m.Magic, m.Rproto, time.Since(start).Round(time.Millisecond))
		}
	}

	stamp, _ := time.Now().MarshalBinary()

	// Broken magic: pingtunnel must ignore it, so anything back is the kernel.
	send("bad magic", &pingtunnel.MyMsg{Type: 1, Key: int32(key), Magic: 12345, Data: stamp, Rproto: 0}, 1500*time.Millisecond)
	send("bad magic", &pingtunnel.MyMsg{Type: 1, Key: int32(key), Magic: 12345, Data: stamp, Rproto: 0}, 1500*time.Millisecond)

	// A genuine ping, twice, and we count everything that comes back.
	valid := func() *pingtunnel.MyMsg {
		st, _ := time.Now().MarshalBinary()
		return &pingtunnel.MyMsg{
			Type:   int32(pingtunnel.MyMsg_PING),
			Key:    int32(key),
			Magic:  int32(pingtunnel.MyMsg_MAGIC),
			Data:   st,
			Rproto: int32(ipv4.ICMPTypeEchoReply),
		}
	}
	send("valid ping", valid(), 2000*time.Millisecond)
	send("valid ping", valid(), 2000*time.Millisecond)

	fmt.Println("--- legend: rproto=0 with untouched payload = kernel echo; rproto=-1 = pingtunnel answer")
}
