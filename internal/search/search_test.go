package search

import (
	"path"
	"testing"
)

func TestIndicesEmptyQuery(t *testing.T) {
	hay := []string{"/clear start a new session", "/resume open a previous session"}
	idx := Indices("", hay)
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 1 {
		t.Fatalf("empty query = %v", idx)
	}
}

func TestFilterFuzzy(t *testing.T) {
	type item struct{ name, desc string }
	items := []item{
		{"model.go", "tui model"},
		{"mainview.go", "main view"},
		{"README.md", "docs"},
	}
	hay := func(it item) string { return path.Base(it.name) + " " + it.name + " " + it.desc }

	all := Filter("", items, hay)
	if len(all) != 3 {
		t.Fatalf("all = %d", len(all))
	}
	// Subsequence: "mv" hits mainview.
	got := Filter("mv", items, hay)
	if len(got) == 0 || got[0].name != "mainview.go" {
		t.Fatalf("mv = %#v", got)
	}
	none := Filter("zzzz", items, hay)
	if len(none) != 0 {
		t.Fatalf("zzzz = %#v", none)
	}
}

func TestFilterNCaps(t *testing.T) {
	items := []string{"a", "aa", "aaa", "b", "c"}
	id := func(s string) string { return s }

	all := FilterN("", items, 0, id)
	if len(all) != 5 {
		t.Fatalf("uncapped empty = %v", all)
	}
	top2 := FilterN("", items, 2, id)
	if len(top2) != 2 || top2[0] != "a" || top2[1] != "aa" {
		t.Fatalf("empty cap = %v", top2)
	}
	// "a" matches a/aa/aaa; cap keeps best-first only.
	capped := FilterN("a", items, 2, id)
	if len(capped) != 2 {
		t.Fatalf("query cap len=%d %v", len(capped), capped)
	}
	none := FilterN("a", items, 0, id)
	if len(none) < 3 {
		t.Fatalf("uncapped query = %v", none)
	}
}

func TestPrefix(t *testing.T) {
	type cmd struct{ name string }
	items := []cmd{
		{name: "clear"},
		{name: "compact"},
		{name: "config"},
		{name: "resume"},
		{name: "review"},
	}
	key := func(c cmd) string { return c.name }

	all := Prefix("", items, 0, key)
	if len(all) != 5 {
		t.Fatalf("empty = %v", all)
	}
	// Prefix preserves definition order.
	c := Prefix("c", items, 0, key)
	if len(c) != 3 || c[0].name != "clear" || c[1].name != "compact" || c[2].name != "config" {
		t.Fatalf("c = %#v", c)
	}
	cl := Prefix("cl", items, 0, key)
	if len(cl) != 1 || cl[0].name != "clear" {
		t.Fatalf("cl = %#v", cl)
	}
	// Case-insensitive.
	if got := Prefix("RE", items, 0, key); len(got) != 2 || got[0].name != "resume" {
		t.Fatalf("RE = %#v", got)
	}
	// Cap.
	if got := Prefix("c", items, 2, key); len(got) != 2 || got[0].name != "clear" {
		t.Fatalf("cap = %#v", got)
	}
	// No fuzzy: "rs" does not hit resume.
	if got := Prefix("rs", items, 0, key); len(got) != 0 {
		t.Fatalf("rs = %#v", got)
	}
	if got := Prefix("zzz", items, 0, key); len(got) != 0 {
		t.Fatalf("zzz = %#v", got)
	}
}

func TestFilterPathsViaFilterN(t *testing.T) {
	// @ picker pattern: basename-first haystack + top-K fuzzy.
	paths := []string{
		"internal/tui/model.go",
		"internal/tui/mainview.go",
		"README.md",
		"cmd/zeta/main.go",
	}
	hay := func(p string) string { return path.Base(p) + " " + p }

	empty := FilterN("", paths, 2, hay)
	if len(empty) != 2 || empty[0] != paths[0] {
		t.Fatalf("empty = %v", empty)
	}
	got := FilterN("main", paths, 5, hay)
	if len(got) == 0 {
		t.Fatal("no matches")
	}
	// Basename-weighted haystack should surface main* files.
	if got[0] != "internal/tui/mainview.go" && got[0] != "cmd/zeta/main.go" {
		t.Fatalf("main rank = %v", got)
	}
	one := FilterN("model", paths, 1, hay)
	if len(one) != 1 || one[0] != "internal/tui/model.go" {
		t.Fatalf("model = %v", one)
	}
	if n := FilterN("zzzz", paths, 5, hay); len(n) != 0 {
		t.Fatalf("zzzz = %v", n)
	}
}
