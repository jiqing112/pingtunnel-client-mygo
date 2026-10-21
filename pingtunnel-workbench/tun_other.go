//go:build !windows

package main

import (
	"errors"
)

// TUN is wired through wintun, which only exists on Windows.
type tunSession struct{}

func startTun(string, string, int) (*tunSession, error) {
	return nil, errors.New("TUN 模式目前只做在 Windows 上")
}

func (t *tunSession) Close() {}

