//go:build !unix

package procmon

import "os/exec"

func configureProcessGroup(cmd *exec.Cmd) {}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func residentGroupBytes(pid int) (int64, error) { return residentBytes(pid) }
