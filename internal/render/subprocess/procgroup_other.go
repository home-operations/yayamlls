//go:build !unix

package subprocess

import "os/exec"

// isolateProcessGroup is a no-op where process groups aren't available; only
// the direct child is killed on cancellation.
func isolateProcessGroup(*exec.Cmd) {}
