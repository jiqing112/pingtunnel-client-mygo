package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// The app does not link pingtunnel: it drives whatever pingtunnel executable
// the user has, the same one their server runs. That keeps the wire protocol
// and the version choice out of this app's hands — a 2023 fork and a 2026
// upstream build are both fine, as long as the exe says which flags it takes.

const clientExeName = "pingtunnel.exe"

var flagLine = regexp.MustCompile(`(?m)^\s+-([a-zA-Z0-9_]+)`)

type clientBinary struct {
	path  string
	flags map[string]bool
	err   error
}

var (
	binMu    sync.Mutex
	binCache map[string]*clientBinary
)

// findBinary looks for pingtunnel.exe where a Windows user would put it.
func findBinary(custom string) (string, error) {
	custom = strings.TrimSpace(custom)
	if custom != "" {
		if st, err := os.Stat(custom); err == nil && !st.IsDir() {
			return custom, nil
		}
		return "", fmt.Errorf("指定的客户端不存在：%s", custom)
	}

	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, clientExeName),
			filepath.Join(dir, "resources", clientExeName),
			filepath.Join(dir, "bin", clientExeName),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, clientExeName),
			filepath.Join(wd, "bin", clientExeName),
			filepath.Join(wd, "..", clientExeName),
			filepath.Join(wd, "..", "bin", clientExeName),
		)
	}
	if data, err := os.UserConfigDir(); err == nil {
		candidates = append(candidates, filepath.Join(data, "pingtunnel", clientExeName))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			if abs, err := filepath.Abs(c); err == nil {
				return abs, nil
			}
			return c, nil
		}
	}
	if p, err := exec.LookPath(clientExeName); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("pingtunnel"); err == nil {
		return p, nil
	}
	return "", errors.New("没有找到 pingtunnel.exe：把客户端放到本程序旁边，或在「日志与高级」里指定它的路径")
}

// loadBinary returns the executable and the flags it understands, which is how
// one build of this app drives every version of the client.
func loadBinary(custom string) *clientBinary {
	path, err := findBinary(custom)
	if err != nil {
		return &clientBinary{err: err}
	}
	binMu.Lock()
	if binCache == nil {
		binCache = map[string]*clientBinary{}
	}
	key := path
	if st, statErr := os.Stat(path); statErr == nil {
		key = fmt.Sprintf("%s|%d", path, st.ModTime().UnixNano())
	}
	if c, ok := binCache[key]; ok {
		binMu.Unlock()
		return c
	}
	binMu.Unlock()

	b := &clientBinary{path: path, flags: probeFlags(path)}
	binMu.Lock()
	binCache[key] = b
	binMu.Unlock()
	return b
}

// probeFlags asks the exe for its usage text: every pingtunnel prints one, and
// the flags it lists are exactly the flags that will not make it exit(2).
func probeFlags(path string) map[string]bool {
	out := map[string]bool{}
	cmd := exec.Command(path, "-h")
	cmd.SysProcAttr = hiddenProcAttr()
	raw, _ := cmd.CombinedOutput()
	for _, m := range flagLine.FindAllStringSubmatch(string(raw), -1) {
		out[m[1]] = true
	}
	// An exe that prints nothing still takes the flags every version has.
	if len(out) == 0 {
		for _, f := range []string{"type", "l", "s", "t", "timeout", "key", "tcp", "nolog", "noprint", "loglevel", "sock5", "maxconn"} {
			out[f] = true
		}
	}
	return out
}

func (b *clientBinary) supports(flag string) bool { return b.flags[flag] }

// args is the command line for one session, built from the flags this exe
// actually has: an old fork has no -congestion, a new build has no -s5filter,
// and passing either would kill the process at startup.
func (b *clientBinary) args(p Profile, s Settings) []string {
	var args []string
	add := func(flag string, values ...string) {
		if !b.supports(flag) {
			return
		}
		args = append(args, "-"+flag)
		args = append(args, values...)
	}

	add("type", "client")
	add("s", p.Server)
	add("l", p.Listen)
	if p.Socks5 == 0 && p.Target != "" {
		add("t", p.Target)
	}
	add("timeout", strconv.Itoa(p.Timeout))
	add("key", strconv.Itoa(p.Key))

	if p.TCPMode == 1 {
		add("tcp", "1")
		if p.Buffer > 0 {
			add("tcp_bs", strconv.Itoa(p.Buffer))
		}
		if p.MaxWin > 0 {
			add("tcp_mw", strconv.Itoa(p.MaxWin))
		}
		if p.ResendMs > 0 {
			add("tcp_rst", strconv.Itoa(p.ResendMs))
		}
		if p.Compress > 0 {
			add("tcp_gz", strconv.Itoa(p.Compress))
		}
		if p.TCPStat == 1 {
			add("tcp_stat", "1")
		}
	} else {
		add("tcp", "0")
	}

	if p.Socks5 == 1 {
		add("sock5", "1")
		if s.Socks5User != "" {
			add("s5user", s.Socks5User)
		}
		if s.Socks5Pass != "" {
			add("s5pass", s.Socks5Pass)
		}
		if s.S5Filter != "" {
			add("s5filter", s.S5Filter)
		}
		if s.S5File != "" {
			add("s5ftfile", s.S5File)
		}
	}
	if p.MaxConn > 0 {
		add("maxconn", strconv.Itoa(p.MaxConn))
	}
	if p.Congestion != "" {
		add("congestion", p.Congestion)
	}
	if p.ICMPAddr != "" {
		add("icmp_l", p.ICMPAddr)
	}
	// The app owns the log: the exe writes to its stdout and never to a file,
	// so the session log in the window is the only log there is.
	add("nolog", "1")
	add("noprint", "0")
	add("loglevel", s.LogLevel)
	return args
}

// sessionConfig is the same session as a pingtunnel JSON config, the shape
// `pingtunnel -c` reads. Written next to the settings so the session can be
// replayed from a terminal, and used as the command line for the exe.
type sessionConfig struct {
	Type         string `json:"type"`
	Listen       string `json:"listen"`
	Target       string `json:"target,omitempty"`
	Server       string `json:"server"`
	Timeout      int    `json:"timeout"`
	Key          int    `json:"key"`
	TCPMode      int    `json:"tcp,omitempty"`
	TCPBuffer    int    `json:"tcp_bs,omitempty"`
	TCPMaxWin    int    `json:"tcp_mw,omitempty"`
	TCPResendMs  int    `json:"tcp_rst,omitempty"`
	TCPCompress  int    `json:"tcp_gz,omitempty"`
	TCPStat      int    `json:"tcp_stat,omitempty"`
	Congestion   string `json:"congestion,omitempty"`
	Sock5        int    `json:"sock5,omitempty"`
	Sock5User    string `json:"s5user,omitempty"`
	Sock5Pass    string `json:"s5pass,omitempty"`
	Sock5Filter  string `json:"s5filter,omitempty"`
	Sock5MapFile string `json:"s5ftfile,omitempty"`
	MaxConn      int    `json:"maxconn,omitempty"`
	NoLog        int    `json:"nolog"`
	NoPrint      int    `json:"noprint"`
	LogLevel     string `json:"loglevel,omitempty"`
	ICMPListen   string `json:"icmp_l,omitempty"`
}

func configFor(p Profile, s Settings) sessionConfig {
	return sessionConfig{
		Type:         "client",
		Listen:       p.Listen,
		Target:       p.Target,
		Server:       p.Server,
		Timeout:      p.Timeout,
		Key:          p.Key,
		TCPMode:      p.TCPMode,
		TCPBuffer:    p.Buffer,
		TCPMaxWin:    p.MaxWin,
		TCPResendMs:  p.ResendMs,
		TCPCompress:  p.Compress,
		TCPStat:      p.TCPStat,
		Congestion:   p.Congestion,
		Sock5:        p.Socks5,
		Sock5User:    s.Socks5User,
		Sock5Pass:    s.Socks5Pass,
		Sock5Filter:  s.S5Filter,
		Sock5MapFile: s.S5File,
		MaxConn:      p.MaxConn,
		NoLog:        1,
		NoPrint:      0,
		LogLevel:     s.LogLevel,
		ICMPListen:   p.ICMPAddr,
	}
}

func writeSessionFile(path string, cfg sessionConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
