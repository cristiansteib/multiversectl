//go:build unix

package server

import (
	"os"
	"syscall"
)

func reexec(exe string, argv []string) error { return syscall.Exec(exe, argv, os.Environ()) }
