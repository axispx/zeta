//go:build darwin

package image

import (
	"bytes"
	"fmt"
	"os/exec"
)

// macOS: osascript + PNGf clipboard class (OpenCode pattern).
func readClipboardImageBytes() ([]byte, error) {
	// Write clipboard PNG to stdout via a short AppleScript bridge.
	script := `
set pngData to missing value
try
	set pngData to the clipboard as «class PNGf»
on error
	return
end try
if pngData is missing value then return
set tmp to (do shell script "mktemp -t zeta-clip")
set f to open for access (POSIX file tmp) with write permission
write pngData to f
close access f
do shell script "cat " & quoted form of tmp & "; rm -f " & quoted form of tmp
`
	cmd := exec.Command("osascript", "-e", script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("clipboard image: %w", err)
	}
	out := stdout.Bytes()
	if len(out) == 0 {
		return nil, ErrNoImage
	}
	return out, nil
}
