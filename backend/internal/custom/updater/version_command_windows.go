//go:build windows

package updater

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

func configureVersionCommand(command *exec.Cmd) {
	command.WaitDelay = time.Second
}

func terminateVersionCommand(command *exec.Cmd) error {
	if command == nil || command.Process == nil {
		return nil
	}
	err := command.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
