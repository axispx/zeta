package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".env", true},
		{".env.local", true},
		{".env.production", true},
		{".env.development.local", true},
		{"app/.env", true},
		{"foo.env", true},
		{".env.example", false},
		{"app/.env.example", false},
		{".envrc", false},
		{"environment.ts", false},
		{"a.go", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := EnvFile(tc.path); got != tc.want {
			t.Errorf("EnvFile(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// The cases here mirror the read-only list's own expectations: a program whose
// flags can write or execute is never safe, whatever the spelling.
func TestSafeCommand(t *testing.T) {
	safe := []string{
		"ls", "ls -la", "/bin/ls -la", "cat go.mod", "head -30 go.mod",
		"tail -5 f.log", "echo hello", "pwd", "wc -l", "grep -rn foo .",
		"nl -nrz Cargo.toml", "base64", "base64 -w0 f",
		"find . -name file.txt", "sed -n 1,5p file.txt", "sed -n 10p",
		"git status", "git branch", "git branch --show-current",
		"git log -p -1", "git diff --stat", "git show HEAD",
	}
	for _, c := range safe {
		if !SafeCommand(c, "/work") {
			t.Errorf("SafeCommand(%q) = false, want true", c)
		}
	}

	unsafe := []string{
		// not on the list
		"", "go test", "npm install", "curl http://x", "rm -rf /",
		"python -c x", "node -e x", "bash -c x", "sort -o out f",
		// a flag changes what the program does
		"base64 -o out.txt", "base64 --output=out.txt",
		"find . -exec rm {} ;", "find . -delete", "find . -fprint out",
		"rg --pre 'sh' foo", "rg --search-zip foo", "rg -z foo",
		"sed -i s/a/b/ f", "sed -n 's/a/b/p' f", "sed -n 1p f extra",
		"git push", "git commit -m x", "git branch -d feature",
		"git branch new-branch", "git log --output=x", "git -C. status",
		"git --git-dir=/tmp/r status", "git -c user.name=x status",
		"git diff --ext-diff", "git show --textconv",
	}
	for _, c := range unsafe {
		if SafeCommand(c, "/work") {
			t.Errorf("SafeCommand(%q) = true, want false", c)
		}
	}
}

// A read-only program still has to reach the user when it names something the
// surrounding gates would have asked about: this list is the sandbox zeta does
// not have, not a licence to read secrets or leave the workspace.
func TestSafeCommandProtectedPaths(t *testing.T) {
	for _, c := range []string{
		"cat .env",
		"cat .env.local",
		"head -30 .env",
		"cat app/.env",
		"cat secrets.env",
		"cat /etc/passwd",
		"ls /var",
		"cat ~/.ssh/id_rsa",
		"cat ~other/x",
		"cd",
		"cd -",
		"cat /tmp/x/.env",
		"head -5 ../outside.txt",
		"grep -r secret ../other",
	} {
		if SafeCommand(c, "/work") {
			t.Errorf("SafeCommand(%q) = true, want false (protected path)", c)
		}
	}
	// Paths inside the workspace are fine.
	for _, c := range []string{
		"cat src/main.go", "head -5 ./README.md", "ls cmd internal",
		"cat .env.example", "grep -rn foo ./internal",
	} {
		if !SafeCommand(c, "/work") {
			t.Errorf("SafeCommand(%q) = false, want true", c)
		}
	}
}

// Read-only queries beyond the first list: they never need a review.
func TestSafeCommandWiderList(t *testing.T) {
	for _, c := range []string{
		"git remote -v", "git remote", "git remote get-url origin",
		"git ls-files", "git ls-files -m internal", "git for-each-ref --format=%(refname) refs/tags",
		"git tag --list", "git tag -l 'v0.*'", "git tag -l --sort=-v:refname", "git tag --list -n3",
		"git rev-parse HEAD", "git rev-parse --show-toplevel", "git blame -L 1,5 -- a.go",
		"git grep -n foo", "git describe --tags", "git merge-base main HEAD",
		"git shortlog -sn", "git cat-file -p HEAD", "git ls-tree HEAD", "git rev-list --count HEAD",
		"git diff --check",
		"go version", "go doc fmt.Println", "go doc -src net/http.Client",
		"go env GOMODCACHE", "go env", "go list ./...", "go list -m -json all",
		"command -v go", "command -V go",
		"sort -u", "sort -rn f", "sort -k2,2 f",
		"diff a b", "jq .name package.json", "sleep 1", "shasum -a 256 f",
		"printf '%s\\n' x", "basename a/b", "realpath .", "test -f go.mod", "du -sh .",
		"strings -a bin/zeta",
	} {
		if !SafeCommand(c, "/work") {
			t.Errorf("SafeCommand(%q) = false, want true", c)
		}
	}

	for _, c := range []string{
		// creating, deleting or moving things
		"git tag v1", "git tag v1 HEAD", "git tag -d v1", "git tag -a v1 -m x", "git tag -l -d v1",
		"git tag --list --delete v1", "git tag -f v1", "git tag",
		"git remote add o url", "git remote remove o", "git remote set-url o u", "git remote rename a b",
		"git remote -v add o u", "git remote show origin", "git remote update",
		// runs another program
		"git grep -O foo", "git grep --open-files-in-pager=sh foo", "git cat-file --filters HEAD:a",
		"git blame --output=x a.go", "git for-each-ref --output=x",
		"go env -w GOFLAGS=-x", "go env -u GOFLAGS", "go list -toolexec=sh ./...", "go list -exec x ./...",
		"go build", "go test ./...", "go run x.go", "go get x", "go mod tidy", "go generate ./...", "go",
		"command go test", "command -p go", "command -v", "command",
		"sort -o out f", "sort -ro out f", "sort --output=out f", "sort --compress-program=sh f",
		"xargs rm", "env", "tree -o out", "git -p log",
	} {
		if SafeCommand(c, "/work") {
			t.Errorf("SafeCommand(%q) = true, want false", c)
		}
	}
}

// A path that resolves inside the workspace is inside however it is spelled;
// scratch space and the module cache are not "outside"; everything else is.
func TestEscapesWorkspace(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	root := filepath.Join(home, "Developer", "app")
	cases := []struct {
		arg  string
		want bool
	}{
		{"src/a.go", false},
		{"./a", false},
		{"a/../b", false},
		{"..", true},
		{"../x", true},
		{"a/../../x", true},
		{root, false},
		{root + "/internal/x.go", false},
		{"~/Developer/app", false},
		{"~/Developer/app/internal", false},
		{"$HOME/Developer/app/x", false},
		{"${HOME}/Developer/app/x", false},
		{"$PWD", false},
		{"$PWD/x", false},
		{"~", true},
		{"~/Developer", true},
		{"~/Developer/app2", true}, // a sibling sharing a prefix
		{"~/.ssh/id_rsa", true},
		{"~other/x", true},
		{"/etc/passwd", true},
		{"/Users", true},
		{"/", true},
		{"/tmp", false},
		{"/tmp/x.log", false},
		{"/private/tmp/x", false},
		{"/tmp/../etc/passwd", true},
		{"/tmp/../Users/x", true},
		{"/var/tmp/x", false},
		{"/var/log/x", true},
		{"/dev/null", false},
		{"/dev/disk0", true},
		{filepath.Join(home, "go", "pkg", "mod", "github.com", "x", "y.go"), false},
		{filepath.Join(home, "go", "src"), true},
		{"~/go/pkg/mod/github.com/x", false},
		{"~/go/pkg/modx", true},
	}
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
	for _, tc := range cases {
		if got := escapesWorkspace(tc.arg, root); got != tc.want {
			t.Errorf("escapesWorkspace(%q) = %v, want %v", tc.arg, got, tc.want)
		}
	}

	// GOMODCACHE and GOPATH move the module cache.
	t.Setenv("GOMODCACHE", "/opt/modcache")
	if escapesWorkspace("/opt/modcache/x", root) || !escapesWorkspace(filepath.Join(home, "go/pkg/mod/x"), root) {
		t.Error("GOMODCACHE should replace the default module cache")
	}
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "/opt/gopath")
	if escapesWorkspace("/opt/gopath/pkg/mod/x", root) {
		t.Error("GOPATH/pkg/mod should be the module cache")
	}

	// An unknown root exempts only scratch space.
	if !escapesWorkspace("~/Developer/app", "") || escapesWorkspace("/tmp/x", "") || !escapesWorkspace("../x", "") {
		t.Error("with no root nothing is inside the workspace")
	}
}

// Dotenv secrets are protected wherever they are, scratch space included.
func TestProtectedPathScratchStillProtectsDotenv(t *testing.T) {
	t.Setenv("GOMODCACHE", "")
	t.Setenv("GOPATH", "")
	for _, c := range []string{"cat /tmp/x/.env", "cat /tmp/.env.local", "cat ~/go/pkg/mod/x/.env"} {
		if !ProtectedPath(c, "/work") {
			t.Errorf("ProtectedPath(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"cat /tmp/x.log", "ls /tmp", "rg -n Foo ~/go/pkg/mod/github.com/x", "head /tmp/.env.example"} {
		if ProtectedPath(c, "/work") {
			t.Errorf("ProtectedPath(%q) = true, want false", c)
		}
	}
}

// A command nothing can split has no argument list, so every word of its text is
// a candidate path and any dotenv name counts.
func TestProtectedText(t *testing.T) {
	root := "/work"
	protected := []string{
		"python3 - <<'PY'\nopen('.env').read()\nPY",
		"python3 - <<'PY'\nopen(\".env.local\")\nPY",
		"cat config/.env > out",
		"node -e \"require('fs').readFileSync('/etc/passwd')\"",
		"python3 -c \"print(open('/etc/hosts').read())\" > out.txt",
		"make $(cat ~/.ssh/config)",
		"cat $HOME/.zshrc > x",
		"cat ../secret > x",
		"cat \"$(cat ../x)\"",
		"echo hi > ~/notes.txt",
		"cp a ~other/b; echo $(date)",
		"x=$(cat /usr/someone/file)",
		"echo hi > /etc/motd",
		"for f in /var/log/*; do cat $f; done",
	}
	for _, c := range protected {
		if !ProtectedText(c, root) {
			t.Errorf("ProtectedText(%q) = false, want true", c)
		}
	}
	free := []string{
		"python3 - <<'PY'\nfrom pathlib import Path\nPath('a.txt').write_text('x')\nPY",
		"go test ./... > /tmp/test.log 2>&1; grep FAIL /tmp/test.log",
		"cd \"$PWD\" && git log --oneline -8",
		"curl -sSL -m 25 -A \"Mozilla/5.0\" \"https://example.com/a/b\" -o /tmp/x.html",
		"echo \"$(date) done\"",
		"python3 -c \"print(10 / 2)\" > out.txt",
		"cat .env.example > out.txt",
		"python3 -c \"import os; print(os.environ['A'])\" > out.txt",
		"x=$(mktemp -d); cd \"$x\" && go mod init check",
		"git -C /work/sub log > /work/sub/log.txt",
		"echo $(git rev-parse --show-toplevel)",
		// slashes in markup and arithmetic are not paths
		"python3 - <<'PY'\nimport re\nh=re.sub(r'<script.*?</script>','',open('a.html').read())\nprint(h.count('</style>'), 7200/3600, (a-b)/3600)\nPY",
		"cd ~/Developer/zeta; make build 2>&1 | tail",
	}
	for _, c := range free {
		// The last one names another project's directory under the home.
		want := c == "cd ~/Developer/zeta; make build 2>&1 | tail"
		if got := ProtectedText(c, root); got != want {
			t.Errorf("ProtectedText(%q) = %v, want %v", c, got, want)
		}
	}
}

// The workspace root reaches the read-only shortcut through the policy, so a
// command naming the workspace by its absolute path runs without a prompt.
func TestPolicyRootMakesWorkspacePathsInside(t *testing.T) {
	root := "/work/app"
	p := Policy{Root: root}
	for command, want := range map[string]Outcome{
		"cd /work/app && git status":     Allow,
		"git -C /work/app status":        Ask, // -C is never read-only
		"ls /work/app/internal":          Allow,
		"cat /work/app/.env":             Ask,
		"cat /work/other/x":              Ask,
		"cat /work/app2/x":               Ask,
		"cd /work/app/../other && ls":    Ask,
		"cat /tmp/x.log | head -5":       Allow,
		"git remote -v":                  Allow,
		"git tag v1":                     Ask,
		"go doc fmt && go version":       Allow,
		"cd":                             Ask,
		"go test ./... && git remote -v": Ask,
		// `cd` is not tracked, so a climbing path is judged from the root: stricter
		// than the shell, never looser.
		"cd /work/app/src && cat ../x":    Ask,
		"cd /work/app/src && cat ../../x": Ask,
	} {
		if got := p.Evaluate(Match{Tool: "bash", Command: command}); got != want {
			t.Errorf("Evaluate(%q) = %v, want %v", command, got, want)
		}
	}
	// Without the root the same absolute path is outside.
	if got := (Policy{}).Evaluate(Match{Tool: "bash", Command: "ls /work/app/internal"}); got != Ask {
		t.Errorf("no root: got %v, want Ask", got)
	}
	// The root is runtime context: it is not saved with the rules.
	if b, _ := json.Marshal(p); strings.Contains(string(b), "work") {
		t.Errorf("root leaked into the saved policy: %s", b)
	}
}
