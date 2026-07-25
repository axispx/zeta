//go:build linux

package image

import (
	"bytes"
	"os"
	"os/exec"
)

func readClipboardImageBytes() ([]byte, error) {
	// Prefer Wayland, then X11.
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		if data, err := tryCmd("wl-paste", "--type", "image/png"); err == nil && len(data) > 0 {
			return data, nil
		}
		if data, err := tryCmd("wl-paste", "--type", "image/jpeg"); err == nil && len(data) > 0 {
			return data, nil
		}
	}
	// xclip targets
	for _, target := range []string{"image/png", "image/jpeg", "image/bmp"} {
		if data, err := tryCmd("xclip", "-selection", "clipboard", "-t", target, "-o"); err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return nil, ErrNoImage
}

func tryCmd(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}
