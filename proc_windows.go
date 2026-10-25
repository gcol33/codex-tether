package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"syscall"
	"time"
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

// configureChild is a no-op on Windows: the job object covers every child.
func configureChild(*exec.Cmd) {}

// forwardSignals is a no-op on Windows, where the app ends its CLI by
// terminating the process and the job object takes the children with it.
func forwardSignals(*os.Process) func() { return func() {} }

// processImage returns the executable path of process pid, or "" when it
// does not exist or cannot be queried.
func processImage(pid int) string {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
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

// bundledCLI returns the newest codex.exe the desktop app has staged under
// %LOCALAPPDATA%\OpenAI\Codex\bin\<hash>\; the app runs that copy rather
// than the one inside its read-only package.
func bundledCLI(string) (string, error) {
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), "OpenAI", "Codex", "bin", "*", "codex.exe"))
	if len(matches) == 0 {
		return "", errors.New(`no staged codex.exe under %LOCALAPPDATA%\OpenAI\Codex\bin (has the Codex app run once?)`)
	}
	sort.Slice(matches, func(i, j int) bool { return modTime(matches[i]).After(modTime(matches[j])) })
	return matches[0], nil
}

func modTime(p string) time.Time {
	st, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
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
