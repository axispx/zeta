package cli

import (
	"strings"
	"testing"
)

func TestParseResume(t *testing.T) {
	t.Parallel()

	opts, err := Parse([]string{"--resume"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Resume || opts.ResumeID != "" {
		t.Fatalf("got resume=%v id=%q", opts.Resume, opts.ResumeID)
	}

	opts, err = Parse([]string{"--resume=abc123"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Resume || opts.ResumeID != "abc123" {
		t.Fatalf("got resume=%v id=%q", opts.Resume, opts.ResumeID)
	}

	_, err = Parse([]string{"--resume", "abc123"})
	if err == nil {
		t.Fatal("expected error for space-separated resume id")
	}
}

func TestParseHelpVersion(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"-h"}, {"--help"}} {
		opts, err := Parse(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !opts.Help {
			t.Fatalf("%v: Help=false", args)
		}
	}
	for _, args := range [][]string{{"-v"}, {"--version"}} {
		opts, err := Parse(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !opts.Version {
			t.Fatalf("%v: Version=false", args)
		}
	}
}

func TestParseUnexpectedArgs(t *testing.T) {
	t.Parallel()
	_, err := Parse([]string{"nope"})
	if err == nil || !strings.Contains(err.Error(), "unexpected") {
		t.Fatalf("got %v", err)
	}
}

func TestWriteVersion(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	WriteVersion(&b)
	if got := b.String(); !strings.HasPrefix(got, "zeta ") || !strings.HasSuffix(got, "\n") {
		t.Fatalf("got %q", got)
	}
}
