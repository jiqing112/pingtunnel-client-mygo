package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// Settings is everything the page lets the user change. Values stay strings
// where pingtunnel takes numbers, so that a half-typed field never jumps back
// to a default while the user is editing it.
type Settings struct {
	Server      string `json:"server"`
	ListenPort  string `json:"listenPort"`
	Target      string `json:"target"`
	Timeout     string `json:"timeout"`
	Key         string `json:"key"`
	TCPMode     bool   `json:"tcpMode"`
	TCPBuffer   string `json:"tcpBuffer"`
	TCPMaxWin   string `json:"tcpMaxWin"`
	TCPResend   string `json:"tcpResend"`
	TCPCompress string `json:"tcpCompress"`
	TCPStat     bool   `json:"tcpStat"`
	Socks5      bool   `json:"socks5"`
	Socks5User  string `json:"socks5User"`
	Socks5Pass  string `json:"socks5Pass"`
	MaxConn     string `json:"maxConn"`
	Congestion  string `json:"congestion"`
	NoLog       bool   `json:"noLog"`
	LogLevel    string `json:"logLevel"`
	ICMPAddr    string `json:"icmpAddr"`
	Encrypt     string `json:"encrypt"`
	EncryptKey  string `json:"encryptKey"`
	BinaryPath  string `json:"binaryPath"`
	S5Filter    string `json:"s5filter"`
	S5File      string `json:"s5ftfile"`
	SystemProxy bool   `json:"systemProxy"`
	HTTPPort    string `json:"httpProxyPort"`
	Tun         bool   `json:"tun"`
	TunMTU      string `json:"tunMtu"`
}

// DefaultSettings is what a fresh install shows: the upstream client's own
// defaults, which also keep the ICMP tunnel working against an unmodified
// server.
func DefaultSettings() Settings {
	return Settings{
		Server:      "",
		ListenPort:  "4455",
		Target:      "",
		Timeout:     "60",
		Key:         "0",
		TCPMode:     true,
		TCPBuffer:   "1048576",
		TCPMaxWin:   "10000",
		TCPResend:   "400",
		TCPCompress: "0",
		TCPStat:     false,
		Socks5:      false,
		MaxConn:     "1000",
		Congestion:  "bb",
		NoLog:       false,
		LogLevel:    "info",
		ICMPAddr:    "",
		Encrypt:     "",
		EncryptKey:  "",
		BinaryPath:  "",
		S5Filter:    "",
		S5File:      "",
		SystemProxy: false,
		HTTPPort:    "34457",
		Tun:         false,
		TunMTU:      "1400",
	}
}

// Profile is the resolved, ready-to-use form of Settings: numbers parsed, the
// pieces NewClient wants, and nothing left for the page to interpret.
type Profile struct {
	Listen      string `json:"listen"`
	Server      string `json:"server"`
	Target      string `json:"target"`
	Timeout     int    `json:"timeout"`
	Key         int    `json:"key"`
	TCPMode     int    `json:"tcpMode"`
	Buffer      int    `json:"buffer"`
	MaxWin      int    `json:"maxWin"`
	ResendMs    int    `json:"resendMs"`
	Compress    int    `json:"compress"`
	TCPStat     int    `json:"tcpStat"`
	Socks5      int    `json:"socks5"`
	MaxConn     int    `json:"maxConn"`
	Congestion  string `json:"congestion"`
	ICMPAddr    string `json:"icmpAddr"`
	Encrypt     string `json:"encrypt"`
	EncryptKey  string `json:"encryptKey"`
	Mode        string `json:"mode"`
	SummaryLine string `json:"summaryLine"`
}

func isPort(v string) bool {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	return err == nil && n > 0 && n < 65536
}

func splitHostPort(v string) (string, string, bool) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(v))
	if err != nil {
		return "", "", false
	}
	return host, port, host != "" && isPort(port)
}

// Normalize cleans up what the user typed and fills the gaps between fields:
// a bare host in target, a missing listen port, and so on.
func (s Settings) Normalize() Settings {
	s.Server = strings.TrimSpace(s.Server)
	s.Server = strings.TrimPrefix(strings.TrimPrefix(s.Server, "icmp://"), "//")
	if _, _, ok := splitHostPort(s.Server); ok {
		// The tunnel is ICMP: only the host matters.
		if host, _, _ := splitHostPort(s.Server); host != "" {
			s.Server = host
		}
	}
	s.ListenPort = strings.TrimPrefix(strings.TrimSpace(s.ListenPort), ":")
	s.Target = strings.TrimSpace(s.Target)
	s.ListenPort = strings.TrimSpace(s.ListenPort)
	s.Key = strings.TrimSpace(s.Key)
	s.Timeout = strings.TrimSpace(s.Timeout)
	for _, f := range []*string{&s.TCPBuffer, &s.TCPMaxWin, &s.TCPResend, &s.TCPCompress, &s.MaxConn} {
		*f = strings.TrimSpace(*f)
	}
	s.LogLevel = strings.ToLower(strings.TrimSpace(s.LogLevel))
	if s.LogLevel == "" {
		s.LogLevel = "info"
	}
	s.BinaryPath = strings.TrimSpace(s.BinaryPath)
	s.S5Filter = strings.ToUpper(strings.TrimSpace(s.S5Filter))
	s.S5File = strings.TrimSpace(s.S5File)
	s.HTTPPort = strings.TrimSpace(s.HTTPPort)
	if !isPort(s.HTTPPort) {
		s.HTTPPort = "34457"
	}
	s.TunMTU = strings.TrimSpace(s.TunMTU)
	if n, err := strconv.Atoi(s.TunMTU); err != nil || n < 576 || n > 9000 {
		s.TunMTU = "1400"
	}
	switch strings.ToLower(strings.TrimSpace(s.Congestion)) {
	case "", "off", "none", "0":
		s.Congestion = "off"
	case "bb":
		s.Congestion = "bb"
	}
	switch strings.ToLower(strings.TrimSpace(s.Encrypt)) {
	case "", "none", "off":
		s.Encrypt = ""
	case "aes128", "aes256", "chacha20":
		s.Encrypt = strings.ToLower(strings.TrimSpace(s.Encrypt))
	}
	if s.Socks5 {
		// socks5 makes the client a local proxy: there is no fixed target.
		s.TCPMode = true
		s.Target = ""
	}
	return s
}

// Validate turns Settings into a Profile, or says exactly which field is wrong.
func (s Settings) Validate() (Profile, error) {
	s = s.Normalize()
	if s.Server == "" {
		return Profile{}, errors.New("请填写服务器地址")
	}
	if !isPort(s.ListenPort) {
		return Profile{}, fmt.Errorf("本地监听端口无效：%q，应为 1-65535", s.ListenPort)
	}
	if !s.Socks5 {
		if s.Target == "" {
			return Profile{}, errors.New("请填写转发目标，或在 socks5 模式下运行")
		}
		if host, port, ok := splitHostPort(s.Target); !ok || host == "" || !isPort(port) {
			return Profile{}, fmt.Errorf("转发目标无效：%q，应形如 127.0.0.1:8080", s.Target)
		}
	}
	timeout, err := strconv.Atoi(s.Timeout)
	if err != nil || timeout <= 0 || timeout > 3600 {
		return Profile{}, fmt.Errorf("超时时间无效：%q，应为 1-3600 秒", s.Timeout)
	}
	key, err := strconv.Atoi(s.Key)
	if err != nil || key < 0 {
		return Profile{}, fmt.Errorf("密钥无效：%q，应为非负整数", s.Key)
	}
	p := Profile{
		Listen:     ":" + s.ListenPort,
		Server:     s.Server,
		Target:     s.Target,
		Timeout:    timeout,
		Key:        key,
		Congestion: s.Congestion,
		ICMPAddr:   strings.TrimSpace(s.ICMPAddr),
		Encrypt:    s.Encrypt,
		EncryptKey: s.EncryptKey,
		Mode:       "udp",
	}
	if s.TCPMode {
		p.Mode = "tcp"
		p.TCPMode = 1
		if p.Buffer, err = atoiAtLeast(s.TCPBuffer, 0, "tcp buffer size"); err != nil {
			return Profile{}, err
		}
		if p.MaxWin, err = atoiAtLeast(s.TCPMaxWin, 0, "tcp max win"); err != nil {
			return Profile{}, err
		}
		if p.ResendMs, err = atoiAtLeast(s.TCPResend, 0, "tcp resend time"); err != nil {
			return Profile{}, err
		}
		if p.Compress, err = atoiAtLeast(s.TCPCompress, 0, "tcp compress"); err != nil {
			return Profile{}, err
		}
		if p.Compress > 9 {
			return Profile{}, errors.New("tcp compress 应为 0-9")
		}
		if s.TCPStat {
			p.TCPStat = 1
		}
	}
	if p.MaxConn, err = atoiAtLeast(s.MaxConn, 1, "max conn"); err != nil {
		return Profile{}, err
	}
	if s.Socks5 {
		p.Socks5 = 1
	}
	p.SummaryLine = profileSummary(p, s)
	return p, nil
}

func atoiAtLeast(v string, min int, name string) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return 0, fmt.Errorf("%s 无效：%q", name, v)
	}
	return n, nil
}

func profileSummary(p Profile, s Settings) string {
	kind := "TCP"
	if p.TCPMode == 0 {
		kind = "UDP"
	}
	role := "端口转发"
	if p.Socks5 == 1 {
		role = "Socks5 代理"
	}
	return fmt.Sprintf("%s · %s · %s", role, kind, "ICMP")
}

// Store keeps settings in the user data directory, in the shape the app reads
// back on start. The directory is resolved on first use: before the app is
// ready MyGo reports no user data path, and a relative one would scatter
// settings.json wherever the app happened to start.
type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore() *Store { return &Store{} }

func (s *Store) Path() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		s.path = resolveSettingsPath()
	}
	return s.path
}

func resolveSettingsPath() string {
	if dir, err := mygo.App.Path(mygo.PathUserData); err == nil && filepath.IsAbs(dir) {
		return filepath.Join(dir, "settings.json")
	}
	// A shell without %AppData% makes Go's own lookup fail, so keep going
	// until one of these gives an absolute path.
	if dir, err := os.UserConfigDir(); err == nil && filepath.IsAbs(dir) {
		return filepath.Join(dir, "pingtunnel", "settings.json")
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		return filepath.Join(home, ".pingtunnel", "settings.json")
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "settings.json")
	}
	return "settings.json"
}

func (s *Store) Load() Settings {
	out := DefaultSettings()
	data, err := os.ReadFile(s.Path())
	if err != nil {
		return out
	}
	// Notepad and PowerShell add a BOM; JSON does not want one.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(data, &out); err != nil {
		return DefaultSettings()
	}
	return out.Normalize()
}

func (s *Store) Save(v Settings) error {
	path := s.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v.Normalize(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SessionPath is the pingtunnel JSON config of the session that runs now, and
// the copy a terminal can replay with pingtunnel -c.
func (s *Store) SessionPath() string {
	return filepath.Join(filepath.Dir(s.Path()), "session.json")
}
