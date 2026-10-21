package frontend

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func runBuildCommand(ctx context.Context, cmd *exec.Cmd) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	job, err := newBuildJob()
	if err != nil {
		return errors.Join(err, ctx.Err())
	}
	defer func() {
		if job != 0 {
			_ = windows.CloseHandle(job)
		}
	}()

	var attrs syscall.SysProcAttr
	if cmd.SysProcAttr != nil {
		attrs = *cmd.SysProcAttr
	}
	attrs.CreationFlags |= windows.CREATE_SUSPENDED
	cmd.SysProcAttr = &attrs
	// A CommandContext watcher could cancel between Start and job assignment.
	// Own cancellation here so no child can run outside the job first.
	cmd.Cancel, cmd.WaitDelay = nil, 0
	if err := cmd.Start(); err != nil {
		return errors.Join(err, ctx.Err())
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		return errors.Join(fmt.Errorf("open frontend build process: %w", err), cmd.Wait(), ctx.Err())
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = cmd.Process.Kill()
		return errors.Join(fmt.Errorf("assign frontend build job: %w", err), cmd.Wait(), ctx.Err())
	}
	if ctx.Err() == nil {
		err = resumeBuildProcess(uint32(cmd.Process.Pid))
	}
	// Wait on the OS process handle before Cmd.Wait: a descendant can inherit
	// stdout and keep its copying goroutines open after the parent exits.
	for err == nil && ctx.Err() == nil {
		var state uint32
		state, err = windows.WaitForSingleObject(process, 25)
		if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
			break
		}
	}
	stopErr := windows.TerminateJobObject(job, 1)
	if stopErr == nil {
		stopErr = waitBuildJob(job)
	}
	if stopErr != nil {
		// KILL_ON_JOB_CLOSE also covers a failed explicit termination/query.
		// Keep the cleanup error instead of reporting a successful build.
		_ = windows.CloseHandle(job)
		job = 0
	}
	return errors.Join(err, stopErr, cmd.Wait(), ctx.Err())
}

func newBuildJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create frontend build job: %w", err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("configure frontend build job: %w", err)
	}
	return job, nil
}

func resumeBuildProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("snapshot frontend build thread: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	err = windows.Thread32First(snapshot, &entry)
	for ; err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return fmt.Errorf("open frontend build thread: %w", err)
		}
		defer windows.CloseHandle(thread)
		if _, err := windows.ResumeThread(thread); err != nil {
			return fmt.Errorf("resume frontend build thread: %w", err)
		}
		return nil
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("find frontend build thread: %w", err)
	}
	return fmt.Errorf("frontend build process %d has no suspended thread", pid)
}

// The basic accounting layout is fixed by JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
// x/sys exposes the information class but does not define this structure.
type buildJobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func waitBuildJob(job windows.Handle) error {
	for {
		var accounting buildJobAccounting
		if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); err != nil {
			return fmt.Errorf("wait for frontend build job: %w", err)
		}
		if accounting.ActiveProcesses == 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}
