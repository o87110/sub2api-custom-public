//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package updater

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func configureVersionCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		return terminateVersionCommand(command)
	}
	command.WaitDelay = time.Second
}

func terminateVersionCommand(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
