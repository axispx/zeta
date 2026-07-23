package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxBashBytes         = 200 * 1024
	defaultBashTimeoutMs = 120_000
	maxBashTimeoutMs     = 600_000
	truncNoteBash        = "\n\n[truncated: showing first %d bytes]"
)

type bashTool struct{}

func (bashTool) Name() string { return "bash" }
func (bashTool) Description() string {
	return "Run a shell command via the user's default shell ($SHELL). " +
		"Working directory defaults to the workspace root; optional workdir must exist inside it " +
		"(cwd convenience only — the command is not sandboxed and can reach the rest of the host). " +
		"Prefer read/edit/grep for file ops; use bash for tests, builds, and process work."
}
func (bashTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Working directory relative to the workspace root (optional, default \".\"; must exist)",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Max runtime in milliseconds (optional, default 120000, max 600000)",
			},
		},
		"required": []string{"command"},
	}
}

func (bashTool) Summary(raw json.RawMessage) string {
	var a struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(raw, &a)
	cmd := strings.TrimSpace(a.Command)
	if cmd == "" {
		return "bash"
	}
	if i := strings.IndexByte(cmd, '\n'); i >= 0 {
		cmd = cmd[:i] + "…"
	}
	if len(cmd) > 80 {
		cmd = cmd[:80] + "…"
	}
	return "bash " + cmd
}

type bashArgs struct {
	Command   string `json:"command"`
	Workdir   string `json:"workdir"`
	TimeoutMs int    `json:"timeout_ms"`
}

func (bashTool) Run(ctx context.Context, root string, raw json.RawMessage) (string, error) {
	var args bashArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(args.Command) == "" {
		return "", fmt.Errorf("command is required")
	}

	workdir := strings.TrimSpace(args.Workdir)
	if workdir == "" {
		workdir = "."
	}
	dir, err := resolvePath(root, workdir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("workdir %q does not exist", workdir)
		}
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir %q is not a directory", workdir)
	}

	timeout := time.Duration(defaultBashTimeoutMs) * time.Millisecond
	if args.TimeoutMs > 0 {
		ms := args.TimeoutMs
		if ms > maxBashTimeoutMs {
			ms = maxBashTimeoutMs
		}
		timeout = time.Duration(ms) * time.Millisecond
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell := userShell()
	buf := &cappedBuffer{limit: maxBashBytes}
	cmd := exec.CommandContext(runCtx, shell, "-c", args.Command)
	cmd.Dir = dir
	cmd.Stdout = buf
	cmd.Stderr = buf
	err = cmd.Run()

	out := buf.String()
	if buf.truncated {
		out += fmt.Sprintf(truncNoteBash, maxBashBytes)
	}

	status := "exit: 0"
	if runCtx.Err() == context.DeadlineExceeded {
		status = fmt.Sprintf("exit: timeout after %s", timeout)
	} else if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			status = fmt.Sprintf("exit: %d", ee.ExitCode())
		} else {
			return "", err
		}
	}
	return withExit(out, status), nil
}

// userShell returns $SHELL when it looks usable, else bash, else sh.
func userShell() string {
	if s := strings.TrimSpace(os.Getenv("SHELL")); s != "" {
		if filepath.IsAbs(s) {
			if _, err := os.Stat(s); err == nil {
				return s
			}
		} else if p, err := exec.LookPath(s); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("bash"); err == nil {
		return p
	}
	return "sh"
}

func withExit(out, status string) string {
	if out != "" {
		return out + "\n" + status
	}
	return status
}

// cappedBuffer keeps at most limit bytes; further writes are discarded.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.truncated {
		return len(p), nil
	}
	remain := c.limit - c.buf.Len()
	if remain <= 0 {
		c.truncated = true
		return len(p), nil
	}
	if len(p) <= remain {
		return c.buf.Write(p)
	}
	_, _ = c.buf.Write(p[:remain])
	c.truncated = true
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	return c.buf.String()
}
