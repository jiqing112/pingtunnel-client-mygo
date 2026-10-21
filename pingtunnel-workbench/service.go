package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// State is what the tunnel itself is doing, as the page shows it.
type State string

const (
	StateIdle     State = "idle"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
	StateError    State = "error"
)

// Status is the whole live picture of a session: what it runs, how fast, and
// how far along.
type Status struct {
	State      State   `json:"state"`
	Message    string  `json:"message"`
	Mode       string  `json:"mode"` // "tcp" or "udp"
	Role       string  `json:"role"` // "forward" or "socks5"
	Server     string  `json:"server"`
	ServerIP   string  `json:"serverIp"`
	Listen     string  `json:"listen"`
	ListenAddr string  `json:"listenAddr"`
	Target     string  `json:"target"`
	Key        int     `json:"key"`
	RTTms      int64   `json:"rttMs"`
	UptimeSec  int64   `json:"uptimeSec"`
	SendKBs    int64   `json:"sendKBs"`
	RecvKBs    int64   `json:"recvKBs"`
	SendPkt    uint64  `json:"sendPkt"`
	RecvPkt    uint64  `json:"recvPkt"`
	SendTotal  uint64  `json:"sendTotalKB"`
	RecvTotal  uint64  `json:"recvTotalKB"`
	Conn       int     `json:"conn"`
	History    []int64 `json:"history"` // recent RTT samples, oldest first
	StartedAt  string  `json:"startedAt"`
	PongAge    int64   `json:"pongAge"` // seconds since the last pong, 0 while fresh
	Pid        int     `json:"pid"`
	ProxyAddr  string  `json:"proxyAddr"`  // local HTTP proxy in front of socks5, when running
	SystemProxy bool   `json:"systemProxy"` // Windows' proxy setting points at ProxyAddr
	ProxyReady  bool   `json:"proxyReady"`  // the tunnel answered recently, so the proxy can work
	Tun         bool   `json:"tun"`         // the whole machine is routed through the tunnel
}

// Info describes the app itself, and the client executable it drives.
type Info struct {
	Version      string `json:"version"`
	Platform     string `json:"platform"`
	SettingsPath string `json:"settingsPath"`
	SessionPath  string `json:"sessionPath"`
	ClientPath   string `json:"clientPath"`
	ClientFlags  int    `json:"clientFlags"`
	Admin        bool   `json:"admin"`
}

// ProbeResult is the answer of the "test server" button.
type ProbeResult struct {
	Host    string  `json:"host"`
	IP      string  `json:"ip"`
	Alive   bool    `json:"alive"`
	Sent    int     `json:"sent"`
	Recv    int     `json:"recv"`
	MinMs   float64 `json:"minMs"`
	AvgMs   float64 `json:"avgMs"`
	MaxMs   float64 `json:"maxMs"`
	Message string  `json:"message"`
}

// Session events, consumed by the page.
var (
	StatusEvent = mygo.NewEvent[Status]("status")
	LogEvent    = mygo.NewEvent[LogBatch]("logs")
)

// Tunnel runs the pingtunnel client as a child process and reads its output.
type Tunnel struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	state    State
	message  string
	profile  Profile
	settings Settings
	loaded   bool
	started  time.Time
	history  []int64
	lastPong time.Time
	rtt      int64
	sendKBs  int64
	recvKBs  int64
	conns    int
	totalUp  uint64
	totalDn  uint64
	pktsUp   uint64
	pktsDn   uint64
	pid      int
	win      *mygo.Window
	logs     *logSink
	store    *Store
	stopStat chan struct{}
	bridge   *windowsProxyBridge
	sysProxy bool
	sysSnap  systemProxySnapshot
	tun      *tunSession
}

var (
	rePong = regexp.MustCompile(`pong from \S+ (\S+)`)
	reNet  = regexp.MustCompile(`send (\d+)Packet/s (\d+)KB/s recv (\d+)Packet/s (\d+)KB/s (\d+)/(\d+)Connections`)
	reANSI = regexp.MustCompile("\x1b\\[[0-9;]*m")
)

func NewTunnel(store *Store, logs *logSink) *Tunnel {
	return &Tunnel{
		state:    StateIdle,
		settings: DefaultSettings(),
		logs:     logs,
		store:    store,
	}
}

func (t *Tunnel) attach(win *mygo.Window) {
	t.mu.Lock()
	t.win = win
	t.mu.Unlock()
}

// Info returns the app's version, where it keeps its files, and which client
// executable it found.
func (t *Tunnel) Info() Info {
	bin := loadBinary(t.Settings().BinaryPath)
	path := bin.path
	if bin.err != nil {
		path = ""
	}
	return Info{
		Version:      appVersion,
		Platform:     platformName(),
		SettingsPath: t.store.Path(),
		SessionPath:  t.store.SessionPath(),
		ClientPath:   path,
		ClientFlags:  len(bin.flags),
		Admin:        canRawICMP(),
	}
}

// Settings returns what is saved, for the form to fill in at start. The file
// is read on first use, once the app knows where its data lives.
func (t *Tunnel) Settings() Settings {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.loaded {
		t.settings = t.store.Load()
		t.loaded = true
	}
	return t.settings
}

// SaveSettings stores the form without starting anything.
func (t *Tunnel) SaveSettings(s Settings) (Settings, error) {
	s = s.Normalize()
	if err := t.store.Save(s); err != nil {
		return s, err
	}
	t.mu.Lock()
	t.settings = s
	t.loaded = true
	t.mu.Unlock()
	t.logs.setFilter(s.LogLevel, s.NoLog)
	return s, nil
}

// ChooseBinary lets the page point at a pingtunnel.exe elsewhere on disk.
func (t *Tunnel) ChooseBinary() (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title:   "选择 pingtunnel 客户端",
		Filters: []mygo.FileFilter{{Name: "可执行文件", Extensions: []string{"exe"}}},
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	s := t.Settings()
	s.BinaryPath = paths[0]
	_, _ = t.SaveSettings(s)
	return paths[0], nil
}

// Status returns the current session state, for a page that just loaded.
func (t *Tunnel) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.statusLocked()
}

func (t *Tunnel) statusLocked() Status {
	st := Status{
		State:     t.state,
		Message:   t.message,
		Mode:      t.profile.Mode,
		Server:    t.profile.Server,
		Listen:    strings.TrimPrefix(t.profile.Listen, ":"),
		Target:    t.profile.Target,
		Key:       t.profile.Key,
		History:   append([]int64{}, t.history...),
		SendKBs:   t.sendKBs,
		RecvKBs:   t.recvKBs,
		SendPkt:   t.pktsUp,
		RecvPkt:   t.pktsDn,
		SendTotal: t.totalUp,
		RecvTotal: t.totalDn,
		Conn:      t.conns,
		RTTms:     t.rtt,
		Pid:       t.pid,
	}
	if st.Mode == "" {
		st.Mode = "tcp"
	}
	st.Role = "forward"
	if t.profile.Socks5 == 1 {
		st.Role = "socks5"
	}
	if st.History == nil {
		st.History = []int64{}
	}
	if t.bridge != nil {
		st.ProxyAddr = t.bridge.Addr()
	}
	st.SystemProxy = t.sysProxy
	st.ProxyReady = t.state == StateRunning && t.rtt > 0 && time.Since(t.lastPong) < 15*time.Second
	st.Tun = t.tun != nil
	if t.state == StateRunning && !t.started.IsZero() {
		st.UptimeSec = int64(time.Since(t.started).Seconds())
		st.StartedAt = t.started.Format(time.RFC3339)
		if t.lastPong.IsZero() {
			st.PongAge = st.UptimeSec
		} else {
			st.PongAge = int64(time.Since(t.lastPong).Seconds())
		}
	}
	return st
}

// Start launches the client with the given settings and returns once it runs.
func (t *Tunnel) Start(s Settings) (Status, error) {
	p, err := s.Validate()
	if err != nil {
		return t.Status(), err
	}
	s = s.Normalize()

	t.mu.Lock()
	if t.cmd != nil {
		t.mu.Unlock()
		return t.Status(), errors.New("隧道已在运行，请先停止")
	}
	t.settings = s
	t.loaded = true
	t.profile = p
	t.state = StateStarting
	t.message = ""
	t.history = nil
	t.rtt = 0
	t.lastPong = time.Time{}
	t.sendKBs, t.recvKBs, t.conns = 0, 0, 0
	t.totalUp, t.totalDn, t.pktsUp, t.pktsDn = 0, 0, 0, 0
	t.mu.Unlock()
	t.logs.setFilter(s.LogLevel, s.NoLog)
	t.emitStatus()

	bin := loadBinary(s.BinaryPath)
	if bin.err != nil {
		t.fail(bin.err.Error())
		return t.Status(), bin.err
	}

	// A client killed from the outside leaves its child holding the port; the
	// next start would fail with a bare "bind: Only one usage...". Take the port
	// back when the holder is our own binary, and explain it when it is not.
	if pid := portOwnerPID(s.ListenPort); pid != 0 {
		holder := processImagePath(pid)
		if sameExecutable(holder, bin.path) {
			if err := killProcess(pid); err == nil {
				t.logs.debugf("已清理上次残留的客户端进程（PID %d）", pid)
				time.Sleep(300 * time.Millisecond)
			}
		} else {
			name := filepath.Base(strings.TrimSpace(holder))
			if name == "." || name == "" {
				name = "未知程序"
			}
			msg := fmt.Sprintf("本地端口 %s 已被 %s（PID %d）占用：结束它，或在「连接」里换一个端口", s.ListenPort, name, pid)
			t.fail(msg)
			return t.Status(), errors.New(msg)
		}
	}

	cfgPath := t.store.SessionPath()
	if err := writeSessionFile(cfgPath, configFor(p, s)); err != nil {
		t.logs.debugf("无法写入会话配置：%v", err)
	}
	args := bin.args(p, s)

	cmd := exec.Command(bin.path, args...)
	cmd.SysProcAttr = hiddenProcAttr()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.fail(err.Error())
		return t.Status(), err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.fail(err.Error())
		return t.Status(), err
	}
	if err := cmd.Start(); err != nil {
		msg := fmt.Sprintf("启动 %s 失败：%v", filepath.Base(bin.path), err)
		t.fail(msg)
		return t.Status(), errors.New(msg)
	}

	t.mu.Lock()
	t.cmd = cmd
	t.state = StateRunning
	t.started = time.Now()
	t.pid = cmd.Process.Pid
	t.mu.Unlock()

	t.logs.debugf("客户端进程已启动：%s（pid %d，%d 个可用参数）", filepath.Base(bin.path), cmd.Process.Pid, len(bin.flags))
	t.logs.debugf("会话配置：%s", shortPath(cfgPath))
	t.emitStatus()

	stop := make(chan struct{})
	t.mu.Lock()
	t.stopStat = stop
	t.mu.Unlock()
	go t.readLoop(stdout, "stdout", stop)
	go t.readLoop(stderr, "stderr", stop)
	go t.waitLoop(cmd, stop)
	go t.pumpLoop(stop)

	_ = t.store.Save(s)

	// In socks5 mode the client is a local proxy already; an HTTP bridge in
	// front of it is what Windows' system proxy setting can actually use.
	if p.Socks5 == 1 {
		if bridge, err := startBridge(net.JoinHostPort("127.0.0.1", s.HTTPPort), net.JoinHostPort("127.0.0.1", s.ListenPort)); err == nil {
			t.mu.Lock()
			t.bridge = bridge
			t.mu.Unlock()
			t.logs.debugf("本地 HTTP 代理：http://%s → socks5 127.0.0.1:%s", bridge.Addr(), s.ListenPort)
			if s.SystemProxy {
				if _, err := t.SetSystemProxy(true); err != nil {
					t.logs.debugf("系统代理未开启：%v", err)
				}
			}
		} else {
			t.logs.debugf("本地 HTTP 代理启动失败：%v", err)
		}
	}
	return t.Status(), nil
}

// SetTun routes the whole machine through the tunnel: a wintun adapter, a
// user-space TCP/IP stack, and split default routes. It is the only way to
// cover programs that ignore the system proxy, and it is the fastest way to
// take a machine offline — so it refuses to start until the tunnel has proven
// it carries real bytes.
func (t *Tunnel) SetTun(on bool) (Status, error) {
	if on {
		t.mu.Lock()
		socks := ""
		if t.profile.Socks5 == 1 {
			socks = net.JoinHostPort("127.0.0.1", t.settings.ListenPort)
		}
		server := t.profile.Server
		mtu := 1400
		if n, err := strconv.Atoi(t.settings.TunMTU); err == nil && n > 0 {
			mtu = n
		}
		t.mu.Unlock()

		if socks == "" {
			return t.Status(), errors.New("TUN 全局需要先打开 socks5 模式")
		}
		// A live ping is not enough: an edge device may be answering those.
		// A socks5 CONNECT that completes is the data path working.
		if err := probeDataPath(socks, "www.baidu.com:443", 8*time.Second); err != nil {
			return t.Status(), fmt.Errorf("隧道还没能承载数据，开 TUN 会立刻断网：%w", err)
		}
		session, err := startTun(socks, server, mtu)
		if err != nil {
			return t.Status(), err
		}
		t.mu.Lock()
		t.tun = session
		t.mu.Unlock()
		t.logs.debugf("TUN 已接管全机流量（%s，MTU %d）", tunDeviceName, mtu)
	} else {
		t.mu.Lock()
		session := t.tun
		t.tun = nil
		t.mu.Unlock()
		if session != nil {
			session.Close()
			t.logs.debugf("TUN 已关闭，路由已恢复")
		}
	}
	t.emitStatus()
	return t.Status(), nil
}

// watchdogTun drops the tunnel the moment it stops answering, so a stall does
// not look like a broken computer.
func (t *Tunnel) watchdogTun(pongAge int64) {
	t.mu.Lock()
	session := t.tun
	if session == nil || pongAge < 20 {
		t.mu.Unlock()
		return
	}
	t.tun = nil
	t.mu.Unlock()
	session.Close()
	t.logs.debugf("连续 %d 秒没有回包，TUN 已自动关闭并恢复路由", pongAge)
	t.emitStatus()
}

// SetSystemProxy turns the Windows system proxy on or off. It refuses to point
// the machine at a tunnel that is not answering, because that would simply
// break browsing with no way back except this window.
func (t *Tunnel) SetSystemProxy(on bool) (Status, error) {
	t.mu.Lock()
	bridge := t.bridge
	ready := t.state == StateRunning && t.rtt > 0 && time.Since(t.lastPong) < 15*time.Second
	t.mu.Unlock()

	if on {
		if bridge == nil {
			return t.Status(), errors.New("先在「代理」里打开 socks5，再开系统代理")
		}
		if !ready {
			return t.Status(), errors.New("隧道还没通（没有回包），现在开系统代理会让整台机器上不了网")
		}
		// A live ping is not proof: test that bytes actually flow before
		// pointing the machine at the proxy.
		socks := net.JoinHostPort("127.0.0.1", t.Settings().ListenPort)
		if err := probeDataPath(socks, "www.baidu.com:443", 5*time.Second); err != nil {
			return t.Status(), fmt.Errorf("隧道还没能承载数据，开系统代理会让整机断网：%w", err)
		}
		if err := setSystemProxy(bridge.Addr(), &t.sysSnap); err != nil {
			return t.Status(), err
		}
		t.mu.Lock()
		t.sysProxy = true
		t.mu.Unlock()
		t.logs.debugf("系统代理已指向 http://%s", bridge.Addr())
	} else {
		if err := clearSystemProxy(&t.sysSnap); err != nil {
			return t.Status(), err
		}
		t.mu.Lock()
		t.sysProxy = false
		t.mu.Unlock()
		t.logs.debugf("系统代理已还原")
	}
	t.emitStatus()
	return t.Status(), nil
}

// Stop kills the client process and waits for it.
func (t *Tunnel) Stop() (Status, error) {
	t.mu.Lock()
	cmd := t.cmd
	if cmd == nil {
		t.mu.Unlock()
		return t.Status(), nil
	}
	t.state = StateStopping
	stop := t.stopStat
	t.stopStat = nil
	t.mu.Unlock()
	t.emitStatus()
	if stop != nil {
		close(stop)
	}
	t.logs.debugf("正在停止…")
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	return t.Status(), nil
}

// waitLoop notices the process going away, and says why if it was not asked to.
func (t *Tunnel) waitLoop(cmd *exec.Cmd, stop chan struct{}) {
	err := cmd.Wait()
	select {
	case <-stop:
		// Stopped on purpose.
	default:
	}

	t.mu.Lock()
	wasStopping := t.state == StateStopping
	t.cmd = nil
	t.pid = 0
	if wasStopping {
		t.state = StateIdle
		t.message = ""
	} else {
		t.state = StateError
		// The child prints the real reason (bind failure, bad flag, ...) and
		// then exits; "exit status 2" alone tells nobody anything.
		reason := t.logs.lastError()
		switch {
		case reason != "":
			t.message = "客户端已退出：" + truncate(reason, 160)
		case err != nil:
			t.message = "客户端已退出：" + err.Error()
		default:
			t.message = "客户端已退出"
		}
	}
	t.sendKBs, t.recvKBs, t.conns = 0, 0, 0
	t.started = time.Time{}
	t.rtt = 0
	t.history = nil
	t.lastPong = time.Time{}
	msg := t.message
	t.mu.Unlock()

	if wasStopping {
		t.logs.debugf("隧道已停止")
	} else if msg != "" {
		t.logs.debugf("%s", msg)
	}

	// Never leave the machine pointed at a proxy that just went away.
	t.mu.Lock()
	proxyWasOn := t.sysProxy
	bridge := t.bridge
	t.sysProxy = false
	t.bridge = nil
	t.mu.Unlock()
	if proxyWasOn {
		_ = clearSystemProxy(&t.sysSnap)
		t.logs.debugf("系统代理已还原")
	}
	if bridge != nil {
		bridge.Close()
	}
	t.mu.Lock()
	tunSession := t.tun
	t.tun = nil
	t.mu.Unlock()
	if tunSession != nil {
		tunSession.Close()
		t.logs.debugf("TUN 已关闭，路由已恢复")
	}
	t.emitStatus()
}

// pumpLoop is the bridge to the page: new log lines every 300ms, and the
// dashboard once a second so uptime and "no answer yet" stay live.
func (t *Tunnel) pumpLoop(stop chan struct{}) {
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	last := t.logs.lastSeq()
	n := 0
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			lines := t.logs.since(last)
			if len(lines) > 0 {
				t.mu.Lock()
				win := t.win
				t.mu.Unlock()
				if win != nil {
					if len(lines) > 200 {
						lines = lines[len(lines)-200:]
						_ = LogEvent.Emit(win, LogBatch{Lines: lines, Reset: true})
					} else {
						_ = LogEvent.Emit(win, LogBatch{Lines: lines})
					}
				}
				last = lines[len(lines)-1].Seq
			}
			n++
			if n%3 == 0 {
				if st := t.Status(); st.Tun {
					t.watchdogTun(st.PongAge)
				}
				t.emitStatus()
			}
		}
	}
}

// readLoop turns the client's console output into the session log, and into
// the numbers the dashboard shows.
func (t *Tunnel) readLoop(r io.Reader, source string, stop chan struct{}) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for scanner.Scan() {
		select {
		case <-stop:
			return
		default:
		}
		line := reANSI.ReplaceAllString(scanner.Text(), "")
		if strings.TrimSpace(line) == "" {
			continue
		}
		t.handleLine(line)
	}
}

func (t *Tunnel) handleLine(line string) {
	t.logs.WriteLine(line)

	if m := rePong.FindStringSubmatch(line); m != nil {
		if d, err := time.ParseDuration(m[1]); err == nil {
			ms := d.Milliseconds()
			now := time.Now()
			t.mu.Lock()
			t.rtt = ms
			t.lastPong = now
			t.history = append(t.history, ms)
			if len(t.history) > 90 {
				t.history = t.history[len(t.history)-90:]
			}
			t.mu.Unlock()
			return
		}
	}

	if m := reNet.FindStringSubmatch(line); m != nil {
		sendPkt, _ := strconv.ParseInt(m[1], 10, 64)
		sendKB, _ := strconv.ParseInt(m[2], 10, 64)
		recvPkt, _ := strconv.ParseInt(m[3], 10, 64)
		recvKB, _ := strconv.ParseInt(m[4], 10, 64)
		conns, _ := strconv.Atoi(m[5])
		t.mu.Lock()
		t.sendKBs, t.recvKBs, t.conns = sendKB, recvKB, conns
		t.pktsUp += uint64(max64(sendPkt, 0))
		t.pktsDn += uint64(max64(recvPkt, 0))
		t.totalUp += uint64(max64(sendKB, 0))
		t.totalDn += uint64(max64(recvKB, 0))
		t.mu.Unlock()
	}
}

func max64(v int64, floor int64) int64 {
	if v < floor {
		return floor
	}
	return v
}

func (t *Tunnel) fail(msg string) {
	t.mu.Lock()
	t.cmd = nil
	t.pid = 0
	t.state = StateError
	t.message = msg
	t.mu.Unlock()
	t.logs.debugf("%s", msg)
	t.emitStatus()
}

func (t *Tunnel) emitStatus() {
	t.mu.Lock()
	win := t.win
	st := t.statusLocked()
	t.mu.Unlock()
	if win == nil {
		return
	}
	_ = StatusEvent.Emit(win, st)
}

// Logs returns the session log so far, for a page that just loaded.
func (t *Tunnel) Logs() []LogLine { return t.logs.snapshot() }

// ClearLogs empties the session log.
func (t *Tunnel) ClearLogs() { t.logs.clear() }

// Quit stops the tunnel and closes the app, from the page's EXIT button.
func (t *Tunnel) Quit() {
	go func() {
		_, _ = t.Stop()
		time.Sleep(300 * time.Millisecond)
		mygo.App.Quit()
	}()
}

// Probe pings the server with plain ICMP, before any tunnel exists: the
// fastest way to tell "the host answers" from "pingtunnel answers".
func (t *Tunnel) Probe(host string) (ProbeResult, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return ProbeResult{}, errors.New("请先填写服务器地址")
	}
	if h, _, ok := splitHostPort(host); ok {
		host = h
	}
	ipaddr, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return ProbeResult{Host: host, Message: "无法解析该地址"}, err
	}
	res := ProbeResult{Host: host, IP: ipaddr.String(), Sent: 3}

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return res, errors.New("无法创建 ICMP 连接：请以管理员身份运行（" + err.Error() + "）")
	}
	defer conn.Close()

	id := os.Getpid() & 0xffff
	var samples []float64
	for seq := 1; seq <= 3; seq++ {
		msg := icmp.Message{
			Type: ipv4.ICMPTypeEcho,
			Code: 0,
			Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("pingtunnel-probe")},
		}
		b, err := msg.Marshal(nil)
		if err != nil {
			continue
		}
		start := time.Now()
		if _, err := conn.WriteTo(b, ipaddr); err != nil {
			continue
		}
		_ = conn.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
		buf := make([]byte, 1500)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				break
			}
			reply, err := icmp.ParseMessage(ipv4.ICMPTypeEcho.Protocol(), buf[:n])
			if err != nil || reply.Type != ipv4.ICMPTypeEchoReply {
				continue
			}
			echo, ok := reply.Body.(*icmp.Echo)
			if !ok || echo.ID != id || echo.Seq != seq {
				continue
			}
			samples = append(samples, float64(time.Since(start).Microseconds())/1000.0)
			break
		}
		time.Sleep(120 * time.Millisecond)
	}

	res.Recv = len(samples)
	res.Alive = len(samples) > 0
	if len(samples) > 0 {
		min, max, sum := samples[0], samples[0], 0.0
		for _, v := range samples {
			if v < min {
				min = v
			}
			if v > max {
				max = v
			}
			sum += v
		}
		res.MinMs = round1(min)
		res.MaxMs = round1(max)
		res.AvgMs = round1(sum / float64(len(samples)))
		res.Message = fmt.Sprintf("%d/%d 通", res.Recv, res.Sent)
	} else {
		res.Message = "没有收到 ICMP 回包：服务器可能屏蔽了 ping，或地址有误"
	}
	return res, nil
}

// sameExecutable answers "is this the very binary we launch", which is what
// makes it safe to reclaim a port from a leftover child.
func sameExecutable(a, b string) bool {
	a = strings.ToLower(strings.Trim(strings.TrimSpace(a), `"`))
	b = strings.ToLower(strings.Trim(strings.TrimSpace(b), `"`))
	return a != "" && b != "" && a == b
}

// truncate keeps an error message short enough for the banner.
func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
