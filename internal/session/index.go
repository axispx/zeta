package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const indexFile = "index.json"

// IndexEntry is one row in the per-project session index (for pickers).
type IndexEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name,omitempty"`
	Created string `json:"created,omitempty"`
	Updated string `json:"updated"`
}

// List returns index entries for cwd, newest updated first.
// Missing index yields an empty list (not an error).
func List(cwd string) ([]IndexEntry, error) {
	abs, err := absCwd(cwd)
	if err != nil {
		return nil, err
	}
	entries, err := readIndex(projectDirPath(abs))
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Updated > entries[j].Updated
	})
	return entries, nil
}

// SetName stores the display name in memory and, if indexed, in the project index.
// Before the first Append the name is memory-only; upsertIndex persists it later.
func (s *Session) SetName(name string) error {
	if s == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	s.Name = name
	if s.Path == "" {
		return nil
	}
	dir := filepath.Dir(s.Path)
	entries, err := readIndex(dir)
	if err != nil {
		return err
	}
	for i := range entries {
		if entries[i].ID != s.ID {
			continue
		}
		entries[i].Name = name
		return writeIndex(dir, entries)
	}
	return nil
}

// IndexedName returns the display name from the project index, if any.
func (s *Session) IndexedName() (string, error) {
	if s == nil || s.Path == "" {
		return "", nil
	}
	entries, err := readIndex(filepath.Dir(s.Path))
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.ID == s.ID {
			return e.Name, nil
		}
	}
	return "", nil
}

func (s *Session) upsertIndex() error {
	if s == nil || s.Path == "" {
		return nil
	}
	dir := filepath.Dir(s.Path)
	entries, err := readIndex(dir)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := range entries {
		if entries[i].ID != s.ID {
			continue
		}
		if s.Created != "" {
			entries[i].Created = s.Created
		}
		if s.Name != "" {
			entries[i].Name = s.Name
		}
		entries[i].Updated = now
		return writeIndex(dir, entries)
	}
	created := s.Created
	if created == "" {
		created = now
	}
	entries = append(entries, IndexEntry{
		ID:      s.ID,
		Name:    s.Name,
		Created: created,
		Updated: now,
	})
	return writeIndex(dir, entries)
}

func readIndex(dir string) ([]IndexEntry, error) {
	path := filepath.Join(dir, indexFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session index: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var entries []IndexEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse session index: %w", err)
	}
	return entries, nil
}

func writeIndex(dir string, entries []IndexEntry) error {
	if entries == nil {
		entries = []IndexEntry{}
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session index: %w", err)
	}
	data = append(data, '\n')
	path := filepath.Join(dir, indexFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write session index: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write session index: %w", err)
	}
	return nil
}

// latestFromIndex returns the transcript path for the newest index entry, if any.
func latestFromIndex(dir string) (string, error) {
	entries, err := readIndex(dir)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", nil
	}
	best := entries[0]
	for _, e := range entries[1:] {
		if e.Updated > best.Updated {
			best = e
		}
	}
	path := filepath.Join(dir, best.ID+".jsonl")
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("session %q missing transcript %s", best.ID, path)
		}
		return "", err
	}
	return path, nil
}
