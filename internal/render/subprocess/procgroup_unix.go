//go:build unix

package subprocess

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// isolateProcessGroup makes cancellation kill the child's whole process group,
// so grandchildren of a `sh -c` wrapper don't outlive the render timeout.
func isolateProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		// Cancel only runs after a successful Start; with Setpgid the
		// child's pgid equals its pid.
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
