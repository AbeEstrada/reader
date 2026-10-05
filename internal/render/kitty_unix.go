//go:build unix

package render

import (
	"os"

	"golang.org/x/sys/unix"
)

// cellSize asks /dev/tty, as stdout is a pipe when the output goes to less.
func cellSize() (width, height int) {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return 0, 0
	}
	defer tty.Close()

	ws, err := unix.IoctlGetWinsize(int(tty.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 {
		return 0, 0
	}

	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
}
