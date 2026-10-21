//go:build !windows

package main

import "syscall"

func portOwnerPID(string) int { return 0 }

func processImagePath(int) string { return "" }

func killProcess(int) error { return nil }

func hiddenProcAttr() *syscall.SysProcAttr { return nil }
