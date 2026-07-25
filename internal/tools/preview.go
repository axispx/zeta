package tools

import "encoding/json"

// previewer is an optional Tool capability for permission prompts.
// Implement for tools whose preview differs from Summary (e.g. edit/write diffs).
type previewer interface {
	Preview(root string, raw json.RawMessage) string
}

// Preview returns a side-effect preview for the permission prompt, or "" when
// Summary already carries everything the UI needs (bash command, read path, …).
// Edit/write return a unified diff without writing. Looks up name in ts only
// (does not fall back to Build), matching the active tool set for the turn.
func Preview(ts []Tool, name, root string, raw json.RawMessage) string {
	t, ok := ByName(ts, name)
	if !ok {
		return ""
	}
	if p, ok := t.(previewer); ok {
		return p.Preview(root, raw)
	}
	return ""
}
