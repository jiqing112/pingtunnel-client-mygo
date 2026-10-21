//go:build diag

package main

import (
	"encoding/binary"
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

// TestReplyOnly sends the pingtunnel PING inside an ICMP *echo reply* instead
// of an echo request. The remote kernel does not answer echo replies, so the
// only answer that can come back is pingtunnel's own — which is how we tell
// "the server never replies" from "a middlebox keeps just one of the two
// replies for the same (id, seq)".
//
//	PT_SERVER=host PT_KEY=n go test -tags diag -run TestReplyOnly -v .
func TestReplyOnly(t *testing.T) {
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
	for seq := 1; seq <= 4; seq++ {
		stamp, _ := time.Now().MarshalBinary()
		msg := &pingtunnel.MyMsg{
			Type:   int32(pingtunnel.MyMsg_PING),
			Key:    int32(key),
			Magic:  int32(pingtunnel.MyMsg_MAGIC),
			Data:   stamp,
			Rproto: int32(ipv4.ICMPTypeEchoReply),
		}
		payload, err := proto.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// Type 0: the kernel will not generate a competing reply.
		packet, err := (&icmp.Message{
			Type: ipv4.ICMPTypeEchoReply,
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

		deadline := start.Add(2 * time.Second)
		seen := 0
		for time.Now().Before(deadline) {
			_ = conn.SetReadDeadline(deadline)
			buf := make([]byte, 2048)
			n, src, err := conn.ReadFrom(buf)
			if err != nil {
				break
			}
			m := &pingtunnel.MyMsg{}
			_ = proto.Unmarshal(buf[8:n], m)
			seen++
			t.Logf("seq=%d ← from %s type=%d rproto=%d key=%d magic=%d len=%d (rtt %s)",
				seq, src, buf[0], m.Rproto, m.Key, m.Magic, n, time.Since(start).Round(time.Millisecond))
		}
		if seen == 0 {
			t.Logf("seq=%d: 没有任何回包", seq)
		}
		_ = binary.BigEndian
		time.Sleep(400 * time.Millisecond)
	}
}
