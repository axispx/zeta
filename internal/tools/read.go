package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type readTool struct{}

func (readTool) Name() string { return "read" }
func (readTool) Description() string {
	return "Read a file or list a directory from the workspace. " +
		"For directories, returns immediate children (one per line; trailing / for subdirs). " +
		"For files, optionally slice by 1-based line offset and limit."
}
func (readTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path relative to the workspace root (file or directory)",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "1-based line number to start from when reading a file (optional)",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max number of lines to return when reading a file (optional)",
			},
		},
		"required": []string{"path"},
	}
}

func (readTool) Summary(raw json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &a)
	if a.Path != "" {
		return "read " + a.Path
	}
	return "read"
}

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (readTool) Run(ctx context.Context, root string, raw json.RawMessage) (string, error) {
	_ = ctx
	var args readArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	abs, err := resolvePath(root, args.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		if args.Offset > 0 || args.Limit > 0 {
			return "", fmt.Errorf("offset and limit apply only to files, not directories")
		}
		return listDirImmediate(abs)
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}

	text := string(data)
	lines := strings.Split(text, "\n")
	// Split keeps a trailing empty element when file ends with \n; drop it for numbering.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	start := 0
	if args.Offset > 0 {
		start = args.Offset - 1
		if start > len(lines) {
			start = len(lines)
		}
	}
	end := len(lines)
	if args.Limit > 0 && start+args.Limit < end {
		end = start + args.Limit
	}

	truncated := false
	if end-start > maxReadLines {
		end = start + maxReadLines
		truncated = true
	}

	var b strings.Builder
	nbytes := 0
	for i := start; i < end; i++ {
		line := fmt.Sprintf("%6d|%s\n", i+1, lines[i])
		if nbytes+len(line) > maxReadBytes {
			truncated = true
			break
		}
		b.WriteString(line)
		nbytes += len(line)
	}
	out := strings.TrimSuffix(b.String(), "\n")
	if out == "" && len(lines) == 0 {
		out = "[empty file]"
	}
	if truncated {
		out += fmt.Sprintf(truncNoteRead, maxReadLines, maxReadBytes)
	}
	return out, nil
}

func listDirImmediate(abs string) (string, error) {
	ents, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		name := e.Name()
		if name == ".git" {
			continue
		}
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "[empty directory]", nil
	}
	sort.Slice(names, func(i, j int) bool {
		di, dj := strings.HasSuffix(names[i], "/"), strings.HasSuffix(names[j], "/")
		if di != dj {
			return di
		}
		return names[i] < names[j]
	})
	return strings.Join(names, "\n"), nil
}
