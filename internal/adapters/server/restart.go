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

// restartArgs is how the server is started again: as it was, but on the address
// it is actually on (it may have moved off a busy port, or asked for any), and
// without opening a browser, which was its first start's business.
func restartArgs(args []string, addr string) []string {
	out := []string{}
	for i := 0; i < len(args); i++ {
		a := strings.TrimLeft(args[i], "-")
		switch {
		case args[i] == "--open" || args[i] == "-open" || strings.HasPrefix(a, "open="):
		case a == "addr":
			i++ // its value
		case strings.HasPrefix(a, "addr="):
		default:
			out = append(out, args[i])
		}
	}
	return append(out, "--addr", addr)
}

// restartSoon starts the server again from the file on disk, which an update has
// just replaced. The process keeps its id, so whatever started it still finds it
// where it left it, and the address and token are the ones it had.
func (s *Server) restartSoon() {
	s.restarting.Store(true)
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
	argv := append([]string{exe}, restartArgs(os.Args[1:], s.o.Addr)...)
	time.AfterFunc(restartDelay, func() {
		// Stopped in the meantime (Ctrl-C, `server stop`): it stays stopped.
		if s.runCtx.Err() != nil {
			return
		}
		if err := reexec(exe, argv); err != nil {
			s.restarting.Store(false)
			fmt.Fprintln(os.Stderr, "multiversectl-server: cannot restart after the update; run `multiversectl server restart`:", err)
		}
	})
}
