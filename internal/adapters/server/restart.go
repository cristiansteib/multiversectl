package server

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// restartDelay lets the last lines of the update reach the page before the
// connection goes: the page reads them and then waits for the server to come back.
const restartDelay = 1500 * time.Millisecond

// restartSoon starts the server again from the file on disk, which an update has
// just replaced. The process keeps its id, so whatever started it still finds it
// where it left it, and the address and token are the ones it had.
func (s *Server) restartSoon() {
	if s.o.Restart != nil {
		s.o.Restart()
		return
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "multiversectl-server: cannot restart after the update:", err)
		return
	}
	// The running file was replaced, so the system names it as deleted.
	exe = strings.TrimSuffix(exe, " (deleted)")
	time.AfterFunc(restartDelay, func() {
		if err := reexec(exe); err != nil {
			fmt.Fprintln(os.Stderr, "multiversectl-server: cannot restart after the update; run `multiversectl server restart`:", err)
		}
	})
}
