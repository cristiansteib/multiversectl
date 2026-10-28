//go:build unix

package server

import (
	"os"
	"syscall"
)

func reexec(exe string) error { return syscall.Exec(exe, os.Args, os.Environ()) }
