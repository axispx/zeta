package policy

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
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
	"basename": true, "cat": true, "cd": true, "cmp": true, "cut": true,
	"df": true, "diff": true, "dirname": true, "du": true, "echo": true,
	"expr": true, "false": true, "grep": true, "head": true, "id": true, "jq": true, "ls": true, "md5": true, "md5sum": true, "nl": true,
	"paste": true, "printf": true, "pwd": true, "readlink": true,
	"realpath": true, "rev": true, "seq": true, "sha1sum": true,
	"sha256sum": true, "shasum": true, "sleep": true, "stat": true,
	"strings": true, "sw_vers": true, "tail": true, "test": true, "tr": true,
	"true": true, "uname": true, "uniq": true, "wc": true, "which": true,
	"whoami": true, "[": true,
}

// SafeCommand reports whether one sub-command can run without approval: every
// program it names only reads, it does not name a dotenv secret, and it does not
// reach outside the workspace. A command it rejects is not dangerous, only
// unproven — the caller asks as usual.
//
// This is a guardrail, not a sandbox. It says what a command cannot do; it does
// not consider whether the output is safe to show, so it must never be widened
// to a program whose flags can write or execute.
//
// root is the workspace root; paths inside it are not "outside". Empty means
// only the scratch locations (see ScratchPath) are exempt.
func SafeCommand(command, root string) bool {
	words := strings.Fields(command)
	if len(words) == 0 {
		return false
	}
	return safeWords(words) && !crossesProtectedPath(words, root)
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
	case "sort":
		// -o writes a file and --compress-program runs one; a short-flag cluster
		// containing o (-ro) is -o too.
		return !slices.ContainsFunc(words[1:], func(a string) bool {
			if strings.HasPrefix(a, "--") {
				return a == "--output" || strings.HasPrefix(a, "--output=") ||
					strings.HasPrefix(a, "--compress-program")
			}
			return strings.HasPrefix(a, "-") && strings.Contains(a, "o")
		})
	case "command":
		// `command -v name` looks a program up; `command name` runs it.
		return len(words) > 2 && (words[1] == "-v" || words[1] == "-V")
	case "go":
		return safeGoCommand(words)
	}
	return false
}

// safeGoCommand reports whether a go invocation only reports: its version,
// docs, the package graph, or the environment. `go env -w` writes the config and
// `go list -toolexec` runs a program.
func safeGoCommand(words []string) bool {
	if len(words) < 2 {
		return false
	}
	args := words[2:]
	switch words[1] {
	case "version", "doc":
		return true
	case "env":
		return !slices.ContainsFunc(args, func(a string) bool {
			return a == "-w" || a == "-u" || a == "-changed"
		})
	case "list":
		return !slices.ContainsFunc(args, func(a string) bool {
			return strings.HasPrefix(a, "-toolexec") || strings.HasPrefix(a, "-exec") ||
				strings.HasPrefix(a, "-overlay")
		})
	}
	return false
}

// safeGitCommand reports whether a git invocation is a read-only query. Global
// options come first because they redirect what git reads (--git-dir, -C) or run
// something (-c), which would make any subcommand a gate on the wrong thing.
func safeGitCommand(words []string) bool {
	idx, sub, ok := findGitSubcommand(words, gitReadOnlySubcommands)
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
	switch sub {
	case "branch":
		return gitBranchIsReadOnly(args)
	case "tag":
		return gitTagIsListing(args)
	case "remote":
		return gitRemoteIsListing(args)
	case "grep":
		// -O / --open-files-in-pager runs a command on the matching files.
		return !slices.ContainsFunc(args, func(a string) bool {
			return strings.HasPrefix(a, "-O") || strings.HasPrefix(a, "--open-files-in-pager")
		})
	}
	return true
}

// gitReadOnlySubcommands are the git subcommands that only query the
// repository. branch, tag and remote also create and delete, so their flags are
// checked separately.
var gitReadOnlySubcommands = []string{
	"status", "log", "diff", "show", "branch", "tag", "remote", "ls-files",
	"ls-tree", "for-each-ref", "rev-parse", "rev-list", "blame", "grep",
	"describe", "merge-base", "shortlog", "cat-file",
}

// gitTagIsListing reports whether `git tag` is listing rather than creating or
// deleting: it names -l/--list and nothing but listing options.
func gitTagIsListing(args []string) bool {
	listing := false
	for _, arg := range args {
		switch {
		case arg == "-l" || arg == "--list":
			listing = true
		case !strings.HasPrefix(arg, "-"):
			// A pattern or the commit a filter names; harmless next to --list.
		case slices.Contains([]string{
			"-i", "--ignore-case", "--contains", "--no-contains", "--merged",
			"--no-merged", "--points-at", "--column", "--no-column",
		}, arg):
		case arg == "-n" || strings.HasPrefix(arg, "--sort=") || strings.HasPrefix(arg, "--format="):
		case strings.HasPrefix(arg, "-n") && strings.Trim(arg[2:], "0123456789") == "":
		default:
			return false
		}
	}
	return listing
}

// gitRemoteIsListing reports whether `git remote` only lists remotes or prints
// one's URL.
func gitRemoteIsListing(args []string) bool {
	if len(args) == 0 {
		return true
	}
	if args[0] == "get-url" {
		return true
	}
	for _, arg := range args {
		if arg != "-v" && arg != "--verbose" {
			return false
		}
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
	if slices.Contains([]string{"--output", "--ext-diff", "--textconv", "--exec", "--filters"}, arg) {
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
func crossesProtectedPath(words []string, root string) bool {
	// A bare `cd` (or `cd -`) leaves the workspace without naming a path.
	if programName(words[0]) == "cd" && (len(words) == 1 || words[1] == "-") {
		return true
	}
	for _, w := range words[1:] {
		arg := strings.Trim(w, `"'`)
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		if EnvFile(arg) || escapesWorkspace(arg, root) {
			return true
		}
	}
	return false
}

// ProtectedPath reports whether a sub-command names a dotenv secret or a path
// outside the workspace — the reads a human always sees, whatever else approves
// the command. root is the workspace root.
func ProtectedPath(command, root string) bool {
	words := strings.Fields(command)
	return len(words) > 0 && crossesProtectedPath(words, root)
}

// tokenSplit cuts text at whitespace, quotes, and the punctuation that surrounds
// a path in code, so `open('/etc/passwd')` yields the path.
func tokenSplit(r rune) bool {
	return strings.ContainsRune(" \t\r\n'\"`()[]{},=;|&<>", r)
}

// dotenvMention matches a dotenv file name anywhere in text: `.env`, `.env.local`
// (not `.env.example`, and not `.environ`).
var dotenvMention = regexp.MustCompile(`(^|[^\w])\.env(\.[\w-]+)*([^\w.-]|$)`)

// ProtectedText is ProtectedPath for a command that cannot be split into
// sub-commands (a here-doc script, a substitution, a redirect): there is no
// argument list to read, so every word in the text is a candidate, and any
// mention of a dotenv file counts. It over-reads on purpose, never the reverse.
func ProtectedText(command, root string) bool {
	for _, m := range dotenvMention.FindAllString(command, -1) {
		if !strings.Contains(m, ".env.example") {
			return true
		}
	}
	for _, tok := range strings.FieldsFunc(command, tokenSplit) {
		if tok == "/" || strings.HasPrefix(tok, "-") || strings.HasPrefix(tok, "//") {
			continue // an operator, a flag, or the slashes of a URL
		}
		if strings.HasPrefix(tok, "/") && !rootDirExists(tok) {
			continue // `</script>`, `x/3600`: no such directory, nothing to read
		}
		if EnvFile(tok) || escapesWorkspace(tok, root) {
			return true
		}
	}
	return false
}

// rootDirExists reports whether the first component of the absolute path tok is
// a directory under /. A token in code that merely starts with a slash
// (`/script` from `</script>`, `/3600` from a division) names nothing real.
func rootDirExists(tok string) bool {
	first, _, _ := strings.Cut(strings.TrimPrefix(tok, "/"), "/")
	if first == "" || strings.ContainsAny(first, "*?[") {
		return true // a glob at the top level could match anything
	}
	_, err := os.Lstat("/" + first)
	return err == nil
}

// escapesWorkspace reports whether arg names a path that leaves the workspace:
// absolute, home-relative, or climbing with `..`. root is the workspace root; a
// path that resolves inside it stays inside however it is spelled
// (`~/Developer/app/x` in the app's own workspace). ScratchPath locations never
// count as outside.
func escapesWorkspace(arg, root string) bool {
	arg = expandHome(arg, root)
	if arg == "" {
		return true // `~other`: someone else's home
	}
	if filepath.IsAbs(arg) {
		clean := filepath.Clean(arg)
		return !within(clean, root) && !ScratchPath(clean)
	}
	if !slices.Contains(strings.Split(filepath.ToSlash(arg), "/"), "..") {
		return false
	}
	// Resolve against the root: `a/../b` stays inside, `../b` does not. A `cd`
	// earlier in the command only makes the real position deeper, so this never
	// calls an escape inside.
	return root == "" || !within(filepath.Join(root, arg), root)
}

// expandHome rewrites a leading ~, $HOME, ${HOME} to the home directory, and
// $PWD to the root. It returns "" for ~name, which belongs to another user.
func expandHome(arg, root string) string {
	for _, v := range []string{"$PWD", "${PWD}"} {
		if arg == v || strings.HasPrefix(arg, v+"/") {
			if root == "" {
				return "/"
			}
			return root + arg[len(v):]
		}
	}
	rest, ok := "", false
	for _, v := range []string{"~", "$HOME", "${HOME}"} {
		if arg == v || strings.HasPrefix(arg, v+"/") {
			rest, ok = arg[len(v):], true
			break
		}
	}
	if !ok {
		if strings.HasPrefix(arg, "~") {
			return ""
		}
		return arg
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "/" // unknown home: treat as outside
	}
	return home + rest
}

// within reports whether clean (a cleaned absolute path) is dir or under it.
func within(clean, dir string) bool {
	if dir == "" {
		return false
	}
	dir = filepath.Clean(dir)
	return clean == dir || strings.HasPrefix(clean, dir+string(filepath.Separator))
}

// ScratchPath reports whether abs is somewhere a coding agent routinely reads
// and writes without it being "the user's machine": the OS temp directory, the
// null device, and the Go module cache (public dependency source). Dotenv files
// stay protected wherever they are.
func ScratchPath(abs string) bool {
	abs = filepath.Clean(abs)
	if EnvFile(abs) {
		return false
	}
	switch abs {
	case "/dev/null", "/dev/stdin", "/dev/stdout", "/dev/stderr":
		return true
	}
	for _, dir := range scratchRoots() {
		if within(abs, dir) {
			return true
		}
	}
	return false
}

// scratchOverride replaces the default scratch locations when scratchSet.
var (
	scratchOverride []string
	scratchSet      bool
)

// SetScratchRoots replaces the directories ScratchPath covers, and returns a
// function that restores the defaults. It exists for tests, which use the
// system temp directory as the "outside" place and must not find it exempt.
// Not safe to call while commands are being judged.
func SetScratchRoots(roots ...string) (restore func()) {
	scratchOverride, scratchSet = roots, true
	return func() { scratchOverride, scratchSet = nil, false }
}

// scratchRoots lists the directories ScratchPath covers.
func scratchRoots() []string {
	if scratchSet {
		return scratchOverride
	}
	roots := []string{"/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp"}
	if dir := os.Getenv("GOMODCACHE"); dir != "" {
		roots = append(roots, dir)
	} else if gp := os.Getenv("GOPATH"); gp != "" {
		for _, p := range filepath.SplitList(gp) {
			roots = append(roots, filepath.Join(p, "pkg", "mod"))
		}
	} else if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "go", "pkg", "mod"))
	}
	return roots
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
