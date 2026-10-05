//go:build !unix

package render

func cellSize() (width, height int) {
	return 0, 0
}
