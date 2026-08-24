//go:build unix

package procmon

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	group, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		// Kill descendants first and briefly let the group leader reap them.
		// This avoids accumulating orphaned zombies when the supervisor itself
		// is PID 1 in a Linux container.
		deadline := time.Now().Add(100 * time.Millisecond)
		for {
			members, memberErr := processGroupMembers(group)
			remaining := 0
			if memberErr == nil {
				for _, member := range members {
					if member == cmd.Process.Pid {
						continue
					}
					remaining++
					_ = syscall.Kill(member, syscall.SIGKILL)
				}
			}
			if remaining == 0 || memberErr != nil || time.Now().After(deadline) {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		_ = syscall.Kill(-group, syscall.SIGKILL)
	}
	_ = cmd.Process.Kill()
}

func processGroupMembers(group int) ([]int, error) {
	if runtime.GOOS == "linux" {
		return procGroupMembers(int64(group))
	}
	output, err := exec.Command("ps", "-axo", "pid=,pgid=").Output()
	if err != nil {
		return nil, err
	}
	members := []int{}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		candidate, groupErr := strconv.Atoi(fields[1])
		if pidErr == nil && groupErr == nil && candidate == group {
			members = append(members, pid)
		}
	}
	return members, nil
}

func residentGroupBytes(pid int) (int64, error) {
	group, err := syscall.Getpgid(pid)
	if err != nil {
		return 0, err
	}
	if runtime.GOOS == "linux" {
		if total, procErr := procGroupResidentBytes(int64(group)); procErr == nil {
			return total, nil
		}
	}
	output, err := exec.Command("ps", "-axo", "pgid=,rss=").Output()
	if err != nil {
		return 0, err
	}
	var totalKB int64
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		candidate, parseErr := strconv.Atoi(fields[0])
		if parseErr != nil || candidate != group {
			continue
		}
		rssKB, parseErr := strconv.ParseInt(fields[1], 10, 64)
		if parseErr == nil && rssKB > 0 {
			totalKB += rssKB
		}
	}
	return totalKB * 1024, nil
}

// procGroupResidentBytes sums resident memory for every process in the group by
// scanning /proc, so worker-tree budgets still hold on hosts without procps.
func procGroupResidentBytes(group int64) (int64, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	pageSize := int64(os.Getpagesize())
	var total int64
	for _, entry := range entries {
		if !allDigits(entry.Name()) {
			continue
		}
		stat, readErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if readErr != nil {
			continue
		}
		pgrp, pages, ok := parseProcStat(stat)
		if !ok || pgrp != group {
			continue
		}
		if pages > 0 {
			total += pages * pageSize
		}
	}
	return total, nil
}

func procGroupMembers(group int64) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	members := []int{}
	for _, entry := range entries {
		if !allDigits(entry.Name()) {
			continue
		}
		stat, readErr := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if readErr != nil {
			continue
		}
		pgrp, _, ok := parseProcStat(stat)
		if !ok || pgrp != group {
			continue
		}
		pid, parseErr := strconv.Atoi(entry.Name())
		if parseErr == nil {
			members = append(members, pid)
		}
	}
	return members, nil
}

// parseProcStat reads the process group (field 5) and resident pages (field 24)
// from one /proc/<pid>/stat line; the comm field may contain spaces.
func parseProcStat(stat []byte) (int64, int64, bool) {
	index := bytes.LastIndexByte(stat, ')')
	if index < 0 || index+2 > len(stat) {
		return 0, 0, false
	}
	fields := strings.Fields(string(stat[index+2:]))
	if len(fields) < 22 {
		return 0, 0, false
	}
	pgrp, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	pages, err := strconv.ParseInt(fields[21], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return pgrp, pages, true
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}
