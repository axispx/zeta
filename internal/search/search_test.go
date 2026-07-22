package search

import "testing"

func TestIndicesEmptyQuery(t *testing.T) {
	hay := []string{"/clear start a new session", "/resume open a previous session"}
	idx := Indices("", hay)
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 1 {
		t.Fatalf("empty query = %v", idx)
	}
}

func TestFilterCommands(t *testing.T) {
	type cmd struct{ name, desc string }
	items := []cmd{
		{"/clear", "start a new session"},
		{"/resume", "open a previous session"},
	}
	hay := func(c cmd) string { return c.name + " " + c.desc }

	all := Filter("", items, hay)
	if len(all) != 2 {
		t.Fatalf("all = %d", len(all))
	}
	clear := Filter("cl", items, hay)
	if len(clear) != 1 || clear[0].name != "/clear" {
		t.Fatalf("cl = %#v", clear)
	}
	resume := Filter("op", items, hay)
	if len(resume) != 1 || resume[0].name != "/resume" {
		t.Fatalf("op = %#v", resume)
	}
	byDesc := Filter("new", items, hay)
	if len(byDesc) != 1 || byDesc[0].name != "/clear" {
		t.Fatalf("new = %#v", byDesc)
	}
	ranked := Filter("rs", items, hay)
	if len(ranked) == 0 || ranked[0].name != "/resume" {
		t.Fatalf("rs rank = %#v", ranked)
	}
	none := Filter("foo", items, hay)
	if len(none) != 0 {
		t.Fatalf("foo = %#v", none)
	}
}
