package render

import (
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math/rand/v2"
	"os"
	"strings"
	"sync/atomic"

	"github.com/dolmen-go/kittyimg"
)

// kittyDir keeps image paths short: less -r counts every byte of an escape
// sequence as visible, and splits lines wider than the screen.
var kittyDir = "/tmp"

var kittyID atomic.Uint32

func init() {
	kittyID.Store(rand.Uint32N(240))
}

func encodeKitty(img image.Image, width int) (string, error) {
	path, err := savePNG(img)
	if err != nil {
		return "", err
	}

	cellW, cellH := cellSize()
	cols, rows := kittySize(img.Bounds(), width, cellW, cellH)
	// 16..255 keeps the short color escape, away from the 16 colors that
	// terminals may remap.
	id := kittyID.Add(1)%240 + 16

	// Transmit the PNG file (t=f) with a virtual placement (U=1) of cols x
	// rows cells. q=2 suppresses responses, as less would read them as
	// keystrokes. p=1 makes each resend by less replace the placement.
	seq := fmt.Sprintf("\033_Ga=T,U=1,q=2,f=100,t=f,i=%d,p=1,c=%d,r=%d;%s\033\\",
		id, cols, rows, base64.StdEncoding.EncodeToString([]byte(path)))
	if os.Getenv("TMUX") != "" {
		seq = "\033Ptmux;" + strings.ReplaceAll(seq, "\033", "\033\033") + "\033\\"
	}

	var buf strings.Builder
	buf.WriteString(seq + "\n")
	if err := kittyimg.FprintPlaceholders(&buf, id, cols, rows); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// savePNG leaves the file in place: less sends the escape sequence again, and
// the terminal reads the file again, each time it redraws the line.
func savePNG(img image.Image) (string, error) {
	// image/png writes 16 bits per channel for other types, like JPEG's YCbCr.
	bounds := img.Bounds()
	rgba := image.NewNRGBA(bounds)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)

	file, err := os.CreateTemp(kittyDir, "reader-")
	if err != nil { // No /tmp, as on Windows.
		file, err = os.CreateTemp("", "reader-")
	}
	if err != nil {
		return "", err
	}
	if err := png.Encode(file, rgba); err != nil {
		file.Close()
		os.Remove(file.Name())
		return "", err
	}

	return file.Name(), file.Close()
}

func kittySize(b image.Rectangle, width, cellW, cellH int) (cols, rows int) {
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 10, 20
	}

	// less -r counts the color escapes around each row as visible characters.
	maxCols := max(1, min(width-len("[38;5;255m")-len("[39m"),
		kittyimg.MaxPlaceholderSize))
	maxRows := kittyimg.MaxPlaceholderSize

	cols = (b.Dx() + cellW - 1) / cellW
	rows = (b.Dy() + cellH - 1) / cellH
	if cols > maxCols {
		rows = rows * maxCols / cols
		cols = maxCols
	}
	if rows > maxRows {
		cols = cols * maxRows / rows
		rows = maxRows
	}

	return max(cols, 1), max(rows, 1)
}
