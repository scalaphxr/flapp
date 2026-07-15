//go:build !windows

package youtube

import "os/exec"

func hideWindow(_ *exec.Cmd) {}
