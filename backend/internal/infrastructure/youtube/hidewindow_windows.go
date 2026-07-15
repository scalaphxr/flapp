//go:build windows

package youtube

import (
	"os/exec"
	"syscall"
)

// hideWindow prevents spawned console tools (ffmpeg) from flashing a window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
