//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package shell

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func ioctlGetTermios(fd int) (*unix.Termios, error) {
	return nil, fmt.Errorf("terminal mode is unsupported on this platform")
}

func ioctlSetTermios(fd int, termios *unix.Termios) error {
	_ = fd
	_ = termios
	return fmt.Errorf("terminal mode is unsupported on this platform")
}
