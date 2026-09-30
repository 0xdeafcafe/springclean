package scan

import (
	"syscall"
	"unsafe"
)

// iopolicysys is the syscall behind setiopolicy_np(3).
const (
	sysIopolicysys = 322
	iopolCmdSet    = 1
	iopolTypeDisk  = 0
	iopolScopeProc = 0
	iopolUtility   = 4
)

// BeNice lowers the scan's claim on the machine: the scheduler gives CPU to
// anything else that wants it first, and disk I/O runs in the utility tier
// the way Spotlight and Time Machine do. Idle cores still get used.
func BeNice() {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, 0, 10)
	param := struct{ scope, iotype, policy int32 }{iopolScopeProc, iopolTypeDisk, iopolUtility}
	syscall.Syscall(sysIopolicysys, iopolCmdSet, uintptr(unsafe.Pointer(&param)), 0)
}
