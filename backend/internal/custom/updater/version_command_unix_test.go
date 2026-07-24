//go:build aix || android || darwin || dragonfly || freebsd || hurd || illumos || ios || linux || netbsd || openbsd || solaris

package updater

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBinaryVersionTimeoutKillsProcessGroup(t *testing.T) {
	tempDir := t.TempDir()
	childPIDFile := filepath.Join(tempDir, "child.pid")
	t.Setenv("VERSION_CHILD_PID_FILE", childPIDFile)
	script := filepath.Join(tempDir, "version-probe")
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
sleep 30 &
echo "$!" > "$VERSION_CHILD_PID_FILE"
wait
`), 0700))

	err := validateBinaryVersionWithTimeout(
		t.Context(),
		script,
		"v0.1.162-custom.99",
		100*time.Millisecond,
	)
	require.ErrorContains(t, err, "timed out")

	pidBytes, readErr := os.ReadFile(childPIDFile)
	require.NoError(t, readErr)
	childPID, parseErr := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	require.NoError(t, parseErr)

	require.Eventually(t, func() bool {
		err := syscall.Kill(childPID, 0)
		return errors.Is(err, syscall.ESRCH)
	}, 2*time.Second, 20*time.Millisecond, "version probe child process survived timeout")
}
