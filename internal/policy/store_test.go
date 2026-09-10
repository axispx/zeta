package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	p, err := Load()
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if len(p.Rules) != 0 {
		t.Fatalf("want empty policy, got %+v", p)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	in := Policy{Rules: []Rule{
		{Tool: "bash", CommandPrefix: "go test", Action: ActionAllow},
		{Tool: "edit", Path: "src/a.go", Action: ActionDeny},
	}}
	if err := Save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if data[len(data)-1] != '\n' {
		t.Fatal("want trailing newline")
	}
	out, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out.Rules) != 2 || out.Rules[0] != in.Rules[0] || out.Rules[1] != in.Rules[1] {
		t.Fatalf("round trip: %+v", out)
	}
	info, err := os.Stat(Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode=%o want 600", perm)
	}
}

func TestAddAppendsAndDedupes(t *testing.T) {
	t.Setenv("ZETA_HOME", t.TempDir())
	r := Rule{Tool: "bash", Command: "go test", Action: ActionAllow}
	p, err := Add(r)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(p.Rules) != 1 {
		t.Fatalf("want 1 rule, got %+v", p)
	}
	p, err = Add(r)
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if len(p.Rules) != 1 {
		t.Fatalf("dedupe: got %+v", p)
	}
	// persisted
	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded.Rules) != 1 || loaded.Rules[0] != r {
		t.Fatalf("persist: %+v", loaded)
	}
}

func TestLoadCorruptFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	if err := os.WriteFile(filepath.Join(home, permissionsFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("corrupt file should error")
	}
}

func TestLoadInvalidRule(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ZETA_HOME", home)
	body := `{"rules":[{"tool":"bash","action":"maybe"}]}`
	if err := os.WriteFile(filepath.Join(home, permissionsFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("invalid rule should error")
	}
}
