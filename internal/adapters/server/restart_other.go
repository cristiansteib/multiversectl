//go:build !unix

package server

import "errors"

func reexec(string, []string) error {
	return errors.New("restarting in place is not supported on this system")
}
