//go:build !windows

package main

import "errors"

// System-wide proxying through a Windows setting is a Windows-only idea here;
// elsewhere the app leaves the proxy configuration to the user.
type systemProxySnapshot struct {
	enabled uint32
	server  string
	bypass  string
	haveOld bool
}

func readSystemProxyInto(*systemProxySnapshot) {}

func setSystemProxy(string, *systemProxySnapshot) error {
	return errors.New("系统代理设置目前只做在 Windows 上")
}

func clearSystemProxy(*systemProxySnapshot) error { return nil }

func systemProxyPointsAt(string) bool { return false }

func notifyProxyChanged() {}
