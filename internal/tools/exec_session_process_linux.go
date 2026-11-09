package tools

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func managedExecLeaderExited(pid int) (bool, error) {
	var info unix.Siginfo
	err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT|unix.WNOHANG, nil)
	if errors.Is(err, unix.EINTR) {
		return false, nil
	}
	return info.Signo != 0, err
}

func managedExecGroupLive(pgid int) (bool, error) {
	var live bool
	err := visitManagedExecLinuxGroup(pgid, func(_ int, process managedExecLinuxProcess) bool {
		live = process.live()
		return !live
	})
	return live, err
}

func managedExecStoppedGroup(pgid int) (map[int]managedExecProcessBirth, error) {
	leader, err := readManagedExecLinuxProcess(pgid)
	if err != nil || leader.group != pgid || leader.state != 'T' {
		return nil, err
	}
	group := make(map[int]managedExecProcessBirth)
	allStopped := true
	err = visitManagedExecLinuxGroup(pgid, func(pid int, process managedExecLinuxProcess) bool {
		if !process.live() {
			return true
		}
		// A tracing stop ('t') is not an unattended job-control stop.
		if process.state != 'T' {
			allStopped = false
			return false
		}
		group[pid] = process.birth
		return true
	})
	if err != nil || !allStopped {
		return nil, err
	}
	if birth, ok := group[pgid]; !ok || birth != leader.birth {
		return nil, nil
	}
	current, err := readManagedExecLinuxProcess(pgid)
	if err != nil || current != leader {
		return nil, err
	}
	return group, nil
}

func visitManagedExecLinuxGroup(pgid int, visit func(int, managedExecLinuxProcess) bool) error {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || !entry.IsDir() {
			continue
		}
		process, err := readManagedExecLinuxProcess(pid)
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ESRCH) {
			// A reaped PID can disappear before open or during the stat read.
			continue
		}
		if err != nil {
			return err
		}
		if process.group == pgid && !visit(pid, process) {
			break
		}
	}
	return nil
}

type managedExecLinuxProcess struct {
	group int
	state byte
	birth managedExecProcessBirth
}

func (p managedExecLinuxProcess) live() bool {
	return p.state != 'Z' && p.state != 'X' && p.state != 'x'
}

func readManagedExecLinuxProcess(pid int) (managedExecLinuxProcess, error) {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return managedExecLinuxProcess{}, err
	}
	// The parenthesized command may contain spaces or ')' characters.
	// Fields following the final ')' begin with state, ppid, and pgrp.
	// Field 22 is the process birth tick.
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return managedExecLinuxProcess{}, fmt.Errorf("read process stat: %w", os.ErrInvalid)
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 || len(fields[0]) != 1 {
		return managedExecLinuxProcess{}, fmt.Errorf("read process stat fields: %w", os.ErrInvalid)
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return managedExecLinuxProcess{}, fmt.Errorf("read process group: %w", err)
	}
	tick, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return managedExecLinuxProcess{}, fmt.Errorf("read process birth tick: %w", err)
	}
	return managedExecLinuxProcess{group: group, state: fields[0][0], birth: managedExecProcessBirth{secondsOrTicks: tick}}, nil
}
