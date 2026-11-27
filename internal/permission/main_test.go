package permission

import (
	"os"
	"testing"

	"github.com/axispx/zeta/internal/policy"
)

// These tests use the system temp directory as the place outside the workspace,
// which the scratch exemption would otherwise cover.
func TestMain(m *testing.M) {
	policy.SetScratchRoots()
	os.Exit(m.Run())
}
