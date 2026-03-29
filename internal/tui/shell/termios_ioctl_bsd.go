//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package shell

import "golang.org/x/sys/unix"

func ioctlGetTermios(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, unix.TIOCGETA)
}

func ioctlSetTermios(fd int, termios *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TIOCSETA, termios)
}
