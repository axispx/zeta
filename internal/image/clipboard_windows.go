//go:build windows

package image

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
)

func readClipboardImageBytes() ([]byte, error) {
	// PowerShell: clipboard image → PNG base64 on stdout.
	// Exit 2 = no image (soft miss).
	ps := `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$img = [System.Windows.Forms.Clipboard]::GetImage()
if ($img -eq $null) { exit 2 }
$ms = New-Object System.IO.MemoryStream
$img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
[Convert]::ToBase64String($ms.ToArray())
`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
			return nil, ErrNoImage
		}
		return nil, fmt.Errorf("clipboard image: %w", err)
	}
	s := strings.TrimSpace(stdout.String())
	if s == "" {
		return nil, ErrNoImage
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("clipboard image decode: %w", err)
	}
	return data, nil
}
