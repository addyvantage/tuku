//go:build linux

package shell

import "golang.org/x/sys/unix"

func ioctlGetTermios(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, unix.TCGETS)
}

func ioctlSetTermios(fd int, termios *unix.Termios) error {
	return unix.IoctlSetTermios(fd, unix.TCSETS, termios)
}
