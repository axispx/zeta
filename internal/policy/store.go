package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axispx/zeta/internal/paths"
)

// permissionsFile is the user-owned rule store under ZETA_HOME.
const permissionsFile = "permissions.json"

// Path returns the permissions file path, or "" when the home dir is unknown.
func Path() string {
	home := paths.Home()
	if home == "" {
		return ""
	}
	return filepath.Join(home, permissionsFile)
}

// Load reads the policy. A missing file is an empty policy; a present-but-
// invalid file is an error (unlike the trusted-folder store, which is rewritten
// on accept).
func Load() (Policy, error) {
	path := Path()
	if path == "" {
		return Policy{}, fmt.Errorf("cannot resolve permissions path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Policy{}, nil
		}
		return Policy{}, fmt.Errorf("read permissions: %w", err)
	}
	var p Policy
	if err := json.Unmarshal(data, &p); err != nil {
		return Policy{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := Validate(p.Rules); err != nil {
		return Policy{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return p, nil
}

// Save writes the policy, creating ZETA_HOME when needed. The write is atomic
// (temp file + rename) so a crash or a concurrent zeta cannot leave a truncated
// permissions.json — Load hard-fails on an unparseable file.
func Save(p Policy) error {
	path := Path()
	if path == "" {
		return fmt.Errorf("cannot resolve permissions path")
	}
	if err := paths.EnsureHome(); err != nil {
		return fmt.Errorf("create permissions dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal permissions: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data); err != nil {
		return fmt.Errorf("write permissions: %w", err)
	}
	return nil
}

// writeFileAtomic writes data to path via a same-directory temp file and rename —
// the pattern config/models/session use, with a randomized temp name so two zeta
// processes cannot interleave on a shared "*.tmp".
func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), permissionsFile+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// CreateTemp makes the file 0600, matching the store's permissions.
	return os.Rename(tmp, path)
}

// Add appends r unless an identical rule already exists, then saves and returns
// the updated policy.
func Add(r Rule) (Policy, error) {
	if err := Validate([]Rule{r}); err != nil {
		return Policy{}, err
	}
	p, err := Load()
	if err != nil {
		return Policy{}, err
	}
	for _, existing := range p.Rules {
		if existing == r {
			return p, nil
		}
	}
	p.Rules = append(p.Rules, r)
	if err := Save(p); err != nil {
		return Policy{}, err
	}
	return p, nil
}
