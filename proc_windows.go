package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
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

var job uintptr

// killChildrenOnExit puts this process in a job that kills every member when
// its last handle (ours) closes, so terminating codex-tether also terminates
// the real CLI behind it. Children inherit the job and may still break away
// explicitly, as the daemon launcher does.
func killChildrenOnExit() error {
	h, _, err := procCreateJobObjectW.Call(0, 0)
	if h == 0 {
		return err
	}
	if err := setJobLimits(h, jobKillOnJobClose|jobBreakawayOK); err != nil {
		return err
	}
	self, _, _ := procGetCurrentProcess.Call()
	if r, _, err := procAssignProcessToJobObject.Call(h, self); r == 0 {
		return err
	}
	job = h
	return nil
}

// releaseChildren lets processes the real CLI left behind keep running after
// codex-tether exits normally, as they would had the app run the CLI itself.
func releaseChildren() {
	if job != 0 {
		_ = setJobLimits(job, jobBreakawayOK)
	}
}

func setJobLimits(h uintptr, flags uint32) error {
	var info extendedLimit
	info.Basic.LimitFlags = flags
	if r, _, err := procSetInformationJobObject.Call(h, jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); r == 0 {
		return err
	}
	return nil
}

// configureChild and configureServer are no-ops on Windows: the job object
// covers every child.
func configureChild(*exec.Cmd)  {}
func configureServer(*exec.Cmd) {}

// forwardSignals is a no-op on Windows, where the app ends its CLI by
// terminating the process and the job object takes the children with it.
func forwardSignals(*os.Process, bool) func() { return func() {} }

// stopServer kills the server; the processes it started stay in the job and
// end when codex-tether exits.
func stopServer(p *os.Process) { _ = p.Kill() }

// processStart is not recorded on Windows: the job object already stops a
// server whose codex-tether is gone.
func processStart(int) string { return "" }

func stopOrphanedServer(endpoint) {}

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

// bundledCLI returns the codex.exe in the resources folder of the app process
// appExe, the copy shipped in the same package as the app and therefore
// speaking its protocol version.
func bundledCLI(appExe string) (string, error) {
	if appExe == "" {
		return "", errors.New("no desktop app process to locate its codex.exe from")
	}
	p := filepath.Join(resourcesDir(appExe, "windows"), "codex.exe")
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
		return p, nil
	}
	return "", errors.New("no codex.exe in the resources of " + appExe)
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
