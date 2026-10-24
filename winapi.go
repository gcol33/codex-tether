package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"syscall"
	"unsafe"
)

var (
	kernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW          = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject   = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject  = kernel32.NewProc("AssignProcessToJobObject")
	procGetCurrentProcess         = kernel32.NewProc("GetCurrentProcess")
	procOpenProcess               = kernel32.NewProc("OpenProcess")
	procQueryFullProcessImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle               = kernel32.NewProc("CloseHandle")
)

const (
	jobObjectExtendedLimitInformation = 9
	jobKillOnJobClose                 = 0x2000
	jobBreakawayOK                    = 0x800
	processQueryLimitedInformation    = 0x1000
)

type ioCounters struct{ a, b, c, d, e, f uint64 }

type basicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type extendedLimit struct {
	Basic                 basicLimit
	Io                    ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// killChildrenOnExit puts this process in a job that kills every member when
// its last handle (ours) closes. Children inherit the job and may still break
// away explicitly, as the daemon launcher does.
func killChildrenOnExit() error {
	job, _, err := procCreateJobObjectW.Call(0, 0)
	if job == 0 {
		return err
	}
	var info extendedLimit
	info.Basic.LimitFlags = jobKillOnJobClose | jobBreakawayOK
	if r, _, err := procSetInformationJobObject.Call(job, jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); r == 0 {
		return err
	}
	self, _, _ := procGetCurrentProcess.Call()
	if r, _, err := procAssignProcessToJobObject.Call(job, self); r == 0 {
		return err
	}
	return nil
}

// parentImagePath returns the executable path of the parent process, or "".
func parentImagePath() string {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(os.Getppid()))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	if r, _, _ := procQueryFullProcessImageName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// restrictToCurrentUser removes inherited ACEs and grants full control to the
// current user only; directories pass the grant on to their contents.
func restrictToCurrentUser(path string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	grant := "*" + u.Uid + ":F"
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		grant = "*" + u.Uid + ":(OI)(CI)F"
	}
	out, err := exec.Command("icacls", path, "/inheritance:r", "/grant:r", grant).CombinedOutput()
	if err != nil {
		return fmt.Errorf("icacls %s: %v: %s", path, err, out)
	}
	return nil
}

func ensurePrivateDir(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return restrictToCurrentUser(dir)
}
