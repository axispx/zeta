package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/axispx/zeta/internal/update"
	"github.com/axispx/zeta/internal/version"
)

func TestRunUpdateDevIsSynthetic(t *testing.T) {
	if !update.IsDev(version.Version) {
		t.Skip("release build: runUpdate would hit GitHub")
	}
	var buf bytes.Buffer
	if err := runUpdate(&buf); err != nil {
		t.Fatalf("runUpdate: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "synthetic update") {
		t.Fatalf("output = %q, want a synthetic-update note", got)
	}
}
