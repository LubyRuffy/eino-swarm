package tools

import "golang.org/x/sys/unix"

// Darwin sys/proc.h values.
const (
	managedExecDarwinStopped = 4          // SSTOP
	managedExecDarwinZombie  = 5          // SZOMB
	managedExecDarwinTraced  = 0x00000800 // P_TRACED
)

func managedExecLeaderExited(pid int) (bool, error) {
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return false, err
	}
	return proc.Proc.P_stat == managedExecDarwinZombie, nil
}

func managedExecGroupLive(pgid int) (bool, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return false, err
	}
	for _, proc := range processes {
		if proc.Proc.P_stat != managedExecDarwinZombie {
			return true, nil
		}
	}
	return false, nil
}

func managedExecStoppedGroup(pgid int) (map[int]managedExecProcessBirth, error) {
	leader, err := unix.SysctlKinfoProc("kern.proc.pid", pgid)
	if err != nil || !managedExecDarwinStoppedMember(leader, pgid) || int(leader.Proc.P_pid) != pgid {
		return nil, err
	}
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		return nil, err
	}
	group := make(map[int]managedExecProcessBirth, len(processes))
	for _, proc := range processes {
		if proc.Proc.P_stat == managedExecDarwinZombie {
			continue
		}
		if !managedExecDarwinStoppedMember(&proc, pgid) {
			return nil, nil
		}
		group[int(proc.Proc.P_pid)] = managedExecDarwinBirth(&proc)
	}
	if birth, ok := group[pgid]; !ok || birth != managedExecDarwinBirth(leader) {
		return nil, nil
	}
	current, err := unix.SysctlKinfoProc("kern.proc.pid", pgid)
	if err != nil || !managedExecDarwinStoppedMember(current, pgid) ||
		int(current.Proc.P_pid) != pgid || managedExecDarwinBirth(current) != group[pgid] {
		return nil, err
	}
	return group, nil
}

func managedExecDarwinStoppedMember(proc *unix.KinfoProc, pgid int) bool {
	return proc != nil && proc.Proc.P_pid > 0 && int(proc.Eproc.Pgid) == pgid &&
		proc.Proc.P_stat == managedExecDarwinStopped && proc.Proc.P_flag&managedExecDarwinTraced == 0
}

func managedExecDarwinBirth(proc *unix.KinfoProc) managedExecProcessBirth {
	start := proc.Proc.P_starttime
	return managedExecProcessBirth{secondsOrTicks: start.Sec, microseconds: int64(start.Usec)}
}
