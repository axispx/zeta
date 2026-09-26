package policy

import "testing"

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
		if !SafeCommand(c) {
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
		if SafeCommand(c) {
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
		"ls /tmp",
		"cat ~/.ssh/id_rsa",
		"head -5 ../outside.txt",
		"grep -r secret ../other",
	} {
		if SafeCommand(c) {
			t.Errorf("SafeCommand(%q) = true, want false (protected path)", c)
		}
	}
	// Paths inside the workspace are fine.
	for _, c := range []string{
		"cat src/main.go", "head -5 ./README.md", "ls cmd internal",
		"cat .env.example", "grep -rn foo ./internal",
	} {
		if !SafeCommand(c) {
			t.Errorf("SafeCommand(%q) = false, want true", c)
		}
	}
}
