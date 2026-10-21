//go:build windows

package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	t2sproxy "github.com/xjasonlyu/tun2socks/v2/proxy"
	_ "github.com/xjasonlyu/tun2socks/v2/proxy/socks5"
	"github.com/xjasonlyu/tun2socks/v2/core"
	"github.com/xjasonlyu/tun2socks/v2/core/device"
	"github.com/xjasonlyu/tun2socks/v2/core/device/tun"
	"github.com/xjasonlyu/tun2socks/v2/tunnel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

const (
	// tunDeviceName is the adapter's name in Windows' network list, and how
	// every netsh command below finds it again.
	tunDeviceName = "pingtunnel"
	// tunAddress sits in the benchmarking range, so it cannot collide with
	// anything a LAN or a VPN might hand out.
	tunAddress = "198.18.0.1"
	tunMask    = "255.255.0.0"
)

// tunSession is one live TUN tunnel: the adapter, the user-space stack behind
// it, and the routes this app added so they can be handed back.
type tunSession struct {
	dev    device.Device
	stack  *stack.Stack
	routes []string
}

var (
	reDefaultRoute = regexp.MustCompile(`(?m)^\s*0\.0\.0\.0\s+0\.0\.0\.0\s+(\d+\.\d+\.\d+\.\d+)\s+(\d+\.\d+\.\d+\.\d+)\s+(\d+)\s*$`)
)

// startTun brings the adapter up and points every non-local destination at it.
func startTun(socksAddr, serverIP string, mtu int) (*tunSession, error) {
	if socksAddr == "" {
		return nil, errors.New("TUN 需要 socks5 模式的本地端口")
	}
	proxyURL, err := url.Parse("socks5://" + socksAddr)
	if err != nil {
		return nil, err
	}
	p, err := t2sproxy.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("解析 socks5 代理失败: %w", err)
	}
	tunnel.T().SetProxy(p)

	if mtu <= 0 {
		mtu = 1400
	}
	dev, err := tun.Open(tunDeviceName, uint32(mtu))
	if err != nil {
		return nil, fmt.Errorf("创建 TUN 网卡失败（需要管理员权限，且 wintun.dll 必须在程序目录）: %w", err)
	}
	st, err := core.CreateStack(&core.Config{
		LinkEndpoint:     dev,
		TransportHandler: tunnel.T(),
	})
	if err != nil {
		dev.Close()
		return nil, fmt.Errorf("创建协议栈失败: %w", err)
	}
	s := &tunSession{dev: dev, stack: st}
	if err := s.configure(serverIP); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// configure gives the adapter an address, then installs the routes.
//
// The bypass routes go in first: the tunnel's own packets, the LAN and
// loopback must never be sent back into the tunnel.
func (s *tunSession) configure(serverIP string) error {
	gateway, _, err := defaultRoute()
	if err != nil {
		return err
	}

	run("netsh", "interface", "ipv4", "set", "address", "name="+tunDeviceName,
		"static", tunAddress, tunMask)

	tunIndex, err := interfaceIndex(tunDeviceName)
	if err != nil {
		return err
	}

	bypass := []string{}
	if serverIP != "" {
		bypass = append(bypass, serverIP+"/32")
	}
	bypass = append(bypass, "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8")
	for _, cidr := range bypass {
		ip, mask := cidrToMask(cidr)
		if ip == "" {
			continue
		}
		if runRoute("add", ip, "mask", mask, gateway, "metric", "1") {
			s.routes = append(s.routes, ip)
		}
	}

	// Two halves beat the existing 0.0.0.0/0 on specificity, so all other
	// traffic now goes to the adapter without deleting the user's own route.
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		ip, mask := cidrToMask(cidr)
		if runRoute("add", ip, "mask", mask, "0.0.0.0", "if", tunIndex, "metric", "1") {
			s.routes = append(s.routes, ip)
		}
	}
	return nil
}

// Close takes the routes back and drops the adapter.
func (s *tunSession) Close() {
	for _, ip := range s.routes {
		runRoute("delete", ip)
	}
	s.routes = nil
	if s.stack != nil {
		s.stack.Close()
		s.stack.Wait()
		s.stack = nil
	}
	if s.dev != nil {
		s.dev.Close()
		s.dev = nil
	}
}

// defaultRoute reads the machine's current default gateway.
func defaultRoute() (gateway, ifIndex string, err error) {
	out := output("route", "print", "-4")
	m := reDefaultRoute.FindStringSubmatch(out)
	if m == nil {
		return "", "", errors.New("找不到默认路由，无法安全地接管网络")
	}
	return m[1], m[2], nil
}

// interfaceIndex finds the adapter by name in netsh's interface table.
func interfaceIndex(name string) (string, error) {
	out := output("netsh", "interface", "ipv4", "show", "interfaces")
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if strings.EqualFold(strings.Join(fields[3:], " "), name) {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("找不到网卡 %s（wintun 驱动可能未加载）", name)
}

func cidrToMask(cidr string) (ip, mask string) {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", ""
	}
	return ipnet.IP.String(), net.IP(ipnet.Mask).String()
}

func runRoute(args ...string) bool {
	_, err := exec.Command("route", args...).CombinedOutput()
	return err == nil
}

func run(name string, args ...string) string {
	out, _ := exec.Command(name, args...).CombinedOutput()
	return string(out)
}

func output(name string, args ...string) string {
	out, _ := exec.Command(name, args...).CombinedOutput()
	return string(out)
}

