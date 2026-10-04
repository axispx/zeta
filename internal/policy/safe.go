package policy

import (
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// safePrograms are programs that only read and report: no flag turns one into a
// write, a delete, or a runner of something named in its arguments, so a command
// built from them has nothing for a human to approve. What is absent matters as
// much — `sort` writes with -o, `rg` runs a command with --pre, `find` runs one
// with -exec, and `python` runs whatever it is handed.
var safePrograms = map[string]bool{
	"cat": true, "cd": true, "cut": true, "echo": true, "expr": true,
	"false": true, "grep": true, "head": true, "id": true, "ls": true,
	"nl": true, "paste": true, "pwd": true, "rev": true, "seq": true,
	"stat": true, "tail": true, "tr": true, "true": true, "uname": true,
	"uniq": true, "wc": true, "which": true, "whoami": true,
}

// SafeCommand reports whether one sub-command can run without approval: every
// program it names only reads, it does not name a dotenv secret, and it does not
// reach outside the workspace. A command it rejects is not dangerous, only
// unproven — the caller asks as usual.
//
// This is a guardrail, not a sandbox. It says what a command cannot do; it does
// not consider whether the output is safe to show, so it must never be widened
// to a program whose flags can write or execute.
func SafeCommand(command string) bool {
	words := strings.Fields(command)
	if len(words) == 0 {
		return false
	}
	return safeWords(words) && !crossesProtectedPath(words)
}

// safeWords applies the program list and the per-program option checks.
func safeWords(words []string) bool {
	name := programName(words[0])
	// Both exist on Linux only; elsewhere the bare name is not a program known
	// to read.
	if name == "numfmt" || name == "tac" {
		return runtime.GOOS == "linux"
	}
	if safePrograms[name] {
		return true
	}
	switch name {
	case "base64":
		// -o writes a file, which is the one thing base64 cannot be trusted not
		// to do.
		return !slices.ContainsFunc(words, func(a string) bool {
			return a == "-o" || a == "--output" ||
				strings.HasPrefix(a, "--output=") || (strings.HasPrefix(a, "-o") && a != "-o")
		})
	case "find":
		// Options that execute a command, delete a match, or write path names
		// to a file.
		return !slices.ContainsFunc(words, func(a string) bool {
			return slices.Contains([]string{
				"-exec", "-execdir", "-ok", "-okdir", "-delete",
				"-fls", "-fprint", "-fprint0", "-fprintf",
			}, a)
		})
	case "rg":
		return !slices.ContainsFunc(words, func(a string) bool {
			// --pre and --hostname-bin take a command to run; --search-zip and
			// -z call out to a decompressor.
			if a == "--search-zip" || a == "-z" {
				return true
			}
			for _, opt := range []string{"--pre", "--hostname-bin"} {
				if a == opt || strings.HasPrefix(a, opt+"=") {
					return true
				}
			}
			return false
		})
	case "git":
		return safeGitCommand(words)
	case "sed":
		// Only `sed -n {N|M,N}p`: a line range to print, with no script to run.
		return len(words) > 2 && len(words) <= 4 && words[1] == "-n" && validSedRange(words[2])
	}
	return false
}

// safeGitCommand reports whether a git invocation is a read-only query. Global
// options come first because they redirect what git reads (--git-dir, -C) or run
// something (-c), which would make any subcommand a gate on the wrong thing.
func safeGitCommand(words []string) bool {
	idx, sub, ok := findGitSubcommand(words, []string{"status", "log", "diff", "show", "branch"})
	if !ok {
		return false
	}
	if slices.ContainsFunc(words[1:idx], gitUnsafeGlobalOption) {
		return false
	}
	args := words[idx+1:]
	if slices.ContainsFunc(args, gitUnsafeSubcommandOption) {
		return false
	}
	if sub == "branch" {
		return gitBranchIsReadOnly(args)
	}
	return true
}

// findGitSubcommand finds a wanted subcommand, skipping the global options that
// may precede it. git's first non-option word is the subcommand, so anything
// else ends the scan rather than being mistaken for a later argument.
func findGitSubcommand(words, wanted []string) (int, string, bool) {
	skipNext := false
	for i := 1; i < len(words); i++ {
		if skipNext {
			skipNext = false
			continue
		}
		arg := words[i]
		if gitGlobalOptionWithInlineValue(arg) {
			continue
		}
		if gitGlobalOptionWithValue(arg) {
			skipNext = true
			continue
		}
		if arg == "--" || strings.HasPrefix(arg, "-") {
			continue
		}
		if slices.Contains(wanted, arg) {
			return i, arg, true
		}
		return 0, "", false
	}
	return 0, "", false
}

// gitUnsafeGlobalOption reports an option that changes which repository is read
// or runs a command before the subcommand.
func gitUnsafeGlobalOption(arg string) bool {
	if slices.Contains([]string{
		"-C", "-c", "-p", "--config-env", "--exec-path", "--git-dir",
		"--namespace", "--paginate", "--super-prefix", "--work-tree",
	}, arg) {
		return true
	}
	// -C<path> and -c<name=value> carry their value inline.
	if gitGlobalOptionWithInlineValue(arg) {
		return true
	}
	for _, p := range []string{
		"--config-env=", "--exec-path=", "--git-dir=", "--namespace=",
		"--super-prefix=", "--work-tree=",
	} {
		if strings.HasPrefix(arg, p) {
			return true
		}
	}
	return false
}

// gitGlobalOptionWithValue reports a global option whose value is the next word,
// so a scan for the subcommand has to skip that word too.
func gitGlobalOptionWithValue(arg string) bool {
	return slices.Contains([]string{
		"-C", "-c", "--config-env", "--exec-path", "--git-dir",
		"--namespace", "--super-prefix", "--work-tree",
	}, arg)
}

// gitGlobalOptionWithInlineValue reports a global option carrying its value in
// the same word.
func gitGlobalOptionWithInlineValue(arg string) bool {
	if (strings.HasPrefix(arg, "-C") || strings.HasPrefix(arg, "-c")) && len(arg) > 2 {
		return true
	}
	for _, p := range []string{
		"--config-env=", "--exec-path=", "--git-dir=", "--namespace=",
		"--super-prefix=", "--work-tree=",
	} {
		if strings.HasPrefix(arg, p) {
			return true
		}
	}
	return false
}

// gitUnsafeSubcommandOption reports an option that makes a read-only-looking
// subcommand write to a file or run an external tool.
func gitUnsafeSubcommandOption(arg string) bool {
	if slices.Contains([]string{"--output", "--ext-diff", "--textconv", "--exec"}, arg) {
		return true
	}
	return strings.HasPrefix(arg, "--output=") || strings.HasPrefix(arg, "--exec=")
}

// gitBranchIsReadOnly reports whether `git branch` is listing rather than
// creating, renaming, or deleting.
func gitBranchIsReadOnly(args []string) bool {
	if len(args) == 0 {
		return true // `git branch` alone lists.
	}
	sawReadOnlyFlag := false
	for _, arg := range args {
		switch {
		case slices.Contains([]string{
			"--list", "-l", "--show-current", "-a", "--all",
			"-r", "--remotes", "-v", "-vv", "--verbose",
		}, arg):
			sawReadOnlyFlag = true
		case strings.HasPrefix(arg, "--format="):
			sawReadOnlyFlag = true
		default:
			// Any other flag or positional argument can create, rename, or
			// delete a branch.
			return false
		}
	}
	return sawReadOnlyFlag
}

// validSedRange reports whether arg is a `sed -n` address like `10` or `1,5`,
// optionally carrying the `p` command.
func validSedRange(arg string) bool {
	core, ok := strings.CutSuffix(arg, "p")
	if !ok {
		return false
	}
	parts := strings.Split(core, ",")
	if len(parts) > 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.IndexFunc(p, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return false
		}
	}
	return true
}

// crossesProtectedPath reports whether the command names a file the surrounding
// gates would have asked about: a dotenv secret, or a path outside the
// workspace.
//
// A read-only program is approved here without the sandbox another agent would
// be running it in, so naming one of these still has to reach the user rather
// than read a secret or escape the workspace unasked.
func crossesProtectedPath(words []string) bool {
	for _, w := range words[1:] {
		arg := strings.Trim(w, `"'`)
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		if EnvFile(arg) || escapesWorkspace(arg) {
			return true
		}
	}
	return false
}

// ProtectedPath reports whether a sub-command names a dotenv secret or a path
// outside the workspace — the reads a human always sees, whatever else approves
// the command.
func ProtectedPath(command string) bool {
	words := strings.Fields(command)
	return len(words) > 0 && crossesProtectedPath(words)
}

// escapesWorkspace reports whether arg names a path that leaves the workspace:
// absolute, home-relative, or climbing with `..`.
func escapesWorkspace(arg string) bool {
	if strings.HasPrefix(arg, "/") || strings.HasPrefix(arg, "~") {
		return true
	}
	return slices.Contains(strings.Split(filepath.ToSlash(arg), "/"), "..")
}

// programName strips the directories from a program path, so `/bin/ls -la` is
// recognised the same way as `ls -la`.
func programName(word string) string {
	if i := strings.LastIndexByte(word, '/'); i >= 0 {
		return word[i+1:]
	}
	return word
}

// EnvFile reports whether path is a dotenv secret (.env / .env.*, not
// .env.example). path is '/' -separated (workspace-relative or absolute).
func EnvFile(p string) bool {
	base := path.Base(filepath.ToSlash(p))
	if base == "" || base == "." || base == "/" || base == ".env.example" {
		return false
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	return strings.HasSuffix(base, ".env")
}
