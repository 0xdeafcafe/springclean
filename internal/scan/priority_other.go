//go:build !darwin

package scan

import "syscall"

// BeNice lowers the scan's CPU priority so other work goes first.
func BeNice() { _ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10) }
