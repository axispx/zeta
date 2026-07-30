package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/axispx/zeta/internal/workspace"
)

// ErrTrustDeclined is returned when the user declines folder trust (or input is non-interactive).
var ErrTrustDeclined = errors.New("folder not trusted")

// EnsureTrusted prompts once per untrusted folder before the main app loads.
// No-op when the cwd trust target is already approved. Declining returns ErrTrustDeclined.
func EnsureTrusted() error {
	return ensureTrusted(os.Stdin, os.Stderr)
}

func ensureTrusted(in io.Reader, out io.Writer) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if workspace.IsTrusted(cwd) {
		return nil
	}

	target := workspace.TrustTarget(cwd)
	if target == "" {
		return fmt.Errorf("resolve trust path")
	}
	// Abs cwd for the "subdir of git root" note (TrustTarget is already absolute).
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	abs = filepath.Clean(abs)

	if !isInteractive(in) {
		fmt.Fprintf(out, "zeta: folder not trusted: %s\n", workspace.DisplayPath(target))
		fmt.Fprintf(out, "Run zeta interactively in this directory to approve it.\n")
		return ErrTrustDeclined
	}

	fmt.Fprintln(out, "Trust this folder?")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  You are in %s.\n", workspace.DisplayPath(abs))
	if abs != target {
		fmt.Fprintf(out, "  Note: subdirectory of a Git project — trust applies to the repository root:\n")
		fmt.Fprintf(out, "    %s\n", workspace.DisplayPath(target))
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Do you trust the contents of this directory? Untrusted projects carry a")
	fmt.Fprintln(out, "higher risk of prompt injection via project files (for example AGENTS.md).")
	fmt.Fprintln(out)
	fmt.Fprint(out, "Trust this folder? [y/N] ")

	line, err := readLine(in)
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(out)
			return ErrTrustDeclined
		}
		return err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		return ErrTrustDeclined
	}
	return workspace.Trust(target)
}

func readLine(in io.Reader) (string, error) {
	sc := bufio.NewScanner(in)
	// Trust answers are short; keep the default token size.
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return sc.Text(), nil
}

// isInteractive reports whether in is a terminal. Non-*os.File readers (tests)
// are treated as interactive so answers can be injected.
func isInteractive(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return true
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}
