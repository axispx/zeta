//go:build !darwin && !linux && !windows

package image

func readClipboardImageBytes() ([]byte, error) {
	return nil, ErrNoImage
}
