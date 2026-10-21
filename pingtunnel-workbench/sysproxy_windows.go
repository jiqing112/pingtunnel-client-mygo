//go:build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const inetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

// systemProxySnapshot remembers what the user had, so turning the bridge off
// puts their own proxy back instead of leaving the machine pointed at nothing.
type systemProxySnapshot struct {
	enabled  uint32
	server   string
	bypass   string
	haveOld  bool
}

func readSystemProxyInto(s *systemProxySnapshot) {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if v, _, err := k.GetIntegerValue("ProxyEnable"); err == nil {
		s.enabled = uint32(v)
	}
	if v, _, err := k.GetStringValue("ProxyServer"); err == nil {
		s.server = v
	}
	if v, _, err := k.GetStringValue("ProxyOverride"); err == nil {
		s.bypass = v
	}
	s.haveOld = true
}

// setSystemProxy points Windows at the local bridge.
func setSystemProxy(addr string, keep *systemProxySnapshot) error {
	if keep != nil && !keep.haveOld {
		readSystemProxyInto(keep)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey, registry.SET_VALUE)
	if err != nil {
		k, _, err = registry.CreateKey(registry.CURRENT_USER, inetSettingsKey, registry.SET_VALUE)
		if err != nil {
			return err
		}
	}
	defer k.Close()
	if err := k.SetStringValue("ProxyServer", addr); err != nil {
		return err
	}
	if err := k.SetStringValue("ProxyOverride", "<local>;127.*;10.*;192.168.*"); err != nil {
		return err
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	notifyProxyChanged()
	return nil
}

// clearSystemProxy turns the setting off and, when we know them, restores the
// values that were there before.
func clearSystemProxy(snapshot *systemProxySnapshot) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if snapshot != nil && snapshot.haveOld {
		if err := k.SetDWordValue("ProxyEnable", snapshot.enabled); err != nil {
			return err
		}
		if snapshot.server != "" {
			_ = k.SetStringValue("ProxyServer", snapshot.server)
		}
		if snapshot.bypass != "" {
			_ = k.SetStringValue("ProxyOverride", snapshot.bypass)
		}
	} else {
		if err := k.SetDWordValue("ProxyEnable", 0); err != nil {
			return err
		}
	}
	notifyProxyChanged()
	return nil
}

// systemProxyPointsAt reports whether Windows' proxy is switched on and set to
// addr, which is how the app knows its own toggle state after a restart.
func systemProxyPointsAt(addr string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	enabled, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return false
	}
	server, _, err := k.GetStringValue("ProxyServer")
	return err == nil && server == addr
}

// notifyProxyChanged tells WinINET (Edge, Chrome, Office, most installers) to
// re-read the settings without a logout.
func notifyProxyChanged() {
	wininet := syscall.NewLazyDLL("wininet.dll")
	setOption := wininet.NewProc("InternetSetOptionW")
	const (
		internetOptionSettingsChanged = 39
		internetOptionRefresh         = 37
	)
	_, _, _ = setOption.Call(0, internetOptionSettingsChanged, 0, 0)
	_, _, _ = setOption.Call(0, internetOptionRefresh, 0, 0)
}
