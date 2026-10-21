//go:build windows

package main

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// portOwnerPID returns the pid listening on a TCP port, or 0.
func portOwnerPID(port string) int {
	out, err := exec.Command("netstat", "-ano", "-p", "tcp").Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[len(fields)-2] != "LISTENING" {
			continue
		}
		local := fields[1]
		if !strings.HasSuffix(local, ":"+port) {
			continue
		}
		if pid, err := strconv.Atoi(fields[len(fields)-1]); err == nil {
			return pid
		}
	}
	return 0
}

// processImagePath reads the full path of a running process, which is how the
// app tells "an orphan of ours" from "somebody else's program".
func processImagePath(pid int) string {
	const processQueryLimitedInformation = 0x1000
	h, err := windows.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// killProcess terminates a process and its children are left to Windows.
func killProcess(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.TerminateProcess(h, 1)
}

// hiddenProcAttr starts a child without flashing a console window.
func hiddenProcAttr() *syscall.SysProcAttr {
	const createNoWindow = 0x08000000
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
