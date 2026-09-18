package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/axispx/zeta/internal/ai"
	"github.com/axispx/zeta/internal/image"
	"github.com/axispx/zeta/internal/paths"
)

// Roles stored in message events.
const (
	RoleUser    = "user"
	RoleAgent   = "agent"
	RoleError   = "error"
	RoleTool    = "tool"
	RoleCompact = "compact" // context compaction checkpoint; Text is the summary
)

const (
	typeSession = "session"
	typeMessage = "message"
)

// ToolCall is an assistant-requested function call persisted with an agent turn.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ImageRef is an image on a user turn (data: URL embedded inline).
type ImageRef = image.Ref

// Record is a chat turn loaded from the transcript (message events only).
type Record struct {
	Role       string     `json:"role"`
	Text       string     `json:"text"`
	TS         string     `json:"ts"`
	Images     []ImageRef `json:"images,omitempty"` // user turns only
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Label      string     `json:"label,omitempty"`  // UI label for tool rows
	Tool       string     `json:"tool,omitempty"`   // tool name for RoleTool
	Denied     bool       `json:"denied,omitempty"` // tool call rejected by policy/user
	Tail       int        `json:"tail,omitempty"`   // RoleCompact: API messages retained after checkpoint
	// Usage is the provider's token accounting for this assistant turn. Only
	// agent records carry it; nil when the provider reported none. Kept per
	// turn so /usage can total a resumed session without re-billing anything.
	Usage *ai.Usage `json:"usage,omitempty"`
	// Model is the display name of the model that produced Usage. A session can
	// span models (/model), so /usage needs the attribution to read the totals.
	Model string `json:"model,omitempty"`
	// FramePlan: Plan-mode ingest snapshot; UI frames <proposed_plan> when set.
	// Not re-derived from current mode on resume.
	FramePlan bool `json:"frame_plan,omitempty"`
}

// event is one JSONL line: session header or a message.
type event struct {
	Type       string     `json:"type"`
	ID         string     `json:"id,omitempty"`
	Created    string     `json:"created,omitempty"`
	Role       string     `json:"role,omitempty"`
	Text       string     `json:"text,omitempty"`
	TS         string     `json:"ts,omitempty"`
	Images     []ImageRef `json:"images,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Label      string     `json:"label,omitempty"`
	Tool       string     `json:"tool,omitempty"`
	Denied     bool       `json:"denied,omitempty"`
	Tail       int        `json:"tail,omitempty"`
	Usage      *ai.Usage  `json:"usage,omitempty"`
	Model      string     `json:"model,omitempty"`
	FramePlan  bool       `json:"frame_plan,omitempty"`
}

// Session is an append-only JSONL transcript for one chat.
// A new session is in-memory only until the first Append.
type Session struct {
	ID      string
	Path    string
	Cwd     string
	Created string
	Name    string // display name; hydrated from index on load, set via SetName
	onDisk  bool   // true once the JSONL exists
}

// Persisted reports whether the session transcript exists on disk.
func (s *Session) Persisted() bool {
	return s != nil && s.onDisk
}

// Open resumes the latest session for cwd, or creates a new one if none exist.
func Open(cwd string) (*Session, []Record, error) {
	abs, err := absCwd(cwd)
	if err != nil {
		return nil, nil, err
	}
	dir := projectDirPath(abs)

	latest, err := latestFromIndex(dir)
	if err != nil {
		return nil, nil, err
	}
	if latest == "" {
		return create(abs, dir), nil, nil
	}
	return load(abs, latest)
}

// OpenID resumes a specific session by ID for cwd.
func OpenID(cwd, id string) (*Session, []Record, error) {
	abs, err := absCwd(cwd)
	if err != nil {
		return nil, nil, err
	}
	return load(abs, filepath.Join(projectDirPath(abs), id+".jsonl"))
}

// New creates a new session for cwd (does not resume).
// The transcript file and index entry are created on the first Append.
func New(cwd string) (*Session, error) {
	abs, err := absCwd(cwd)
	if err != nil {
		return nil, err
	}
	return create(abs, projectDirPath(abs)), nil
}

func create(abs, dir string) *Session {
	now := time.Now().UTC()
	id := strings.ReplaceAll(now.Format("20060102-150405.000000000"), ".", "")
	return &Session{
		ID:      id,
		Path:    filepath.Join(dir, id+".jsonl"),
		Cwd:     abs,
		Created: now.Format(time.RFC3339Nano),
	}
}

// Append writes one message event to the session file.
// The first Append creates the JSONL (session header + message) and index entry.
func (s *Session) Append(rec Record) error {
	if s == nil {
		return nil
	}
	if err := s.ensureFile(); err != nil {
		return err
	}
	if rec.TS == "" {
		rec.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := s.writeEvent(event{
		Type:       typeMessage,
		Role:       rec.Role,
		Text:       rec.Text,
		TS:         rec.TS,
		Images:     rec.Images,
		ToolCallID: rec.ToolCallID,
		ToolCalls:  rec.ToolCalls,
		Label:      rec.Label,
		Tool:       rec.Tool,
		Denied:     rec.Denied,
		Tail:       rec.Tail,
		Usage:      rec.Usage,
		Model:      rec.Model,
		FramePlan:  rec.FramePlan,
	}); err != nil {
		return err
	}
	return s.upsertIndex()
}

// DropLastUser removes the last JSONL message when it is a user turn.
// Used to uncommit a prompt cancelled before any model work. If that was the
// only message, the transcript and index entry are removed so the session is
// unpersisted again. Returns false when there is nothing to drop.
func (s *Session) DropLastUser() (bool, error) {
	if s == nil || !s.onDisk || s.Path == "" {
		return false, nil
	}
	lines, lastMsg, lastRole, msgCount, err := readJSONLLines(s.Path)
	if err != nil {
		return false, err
	}
	if lastMsg < 0 || lastRole != RoleUser {
		return false, nil
	}
	keep := append(append([]string{}, lines[:lastMsg]...), lines[lastMsg+1:]...)
	msgCount--
	if msgCount == 0 {
		if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("remove session: %w", err)
		}
		s.onDisk = false
		return true, s.removeIndex()
	}
	if err := writeJSONLLines(s.Path, keep); err != nil {
		return false, err
	}
	return true, s.upsertIndex()
}

func readJSONLLines(path string) (lines []string, lastMsg int, lastRole string, msgCount int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, -1, "", 0, fmt.Errorf("open session: %w", err)
	}
	defer f.Close()

	lastMsg = -1
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), image.MaxJSONLLine)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		lines = append(lines, raw)
		trim := strings.TrimSpace(raw)
		if trim == "" {
			continue
		}
		var evt event
		if err := json.Unmarshal([]byte(trim), &evt); err != nil {
			return nil, -1, "", 0, fmt.Errorf("session %s:%d: %w", filepath.Base(path), lineNo, err)
		}
		if evt.Type == typeMessage {
			lastMsg = len(lines) - 1
			lastRole = evt.Role
			msgCount++
		}
	}
	if err := sc.Err(); err != nil {
		return nil, -1, "", 0, fmt.Errorf("read session: %w", err)
	}
	return lines, lastMsg, lastRole, msgCount, nil
}

func writeJSONLLines(path string, lines []string) error {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	data := []byte(b.String())
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write session: %w", err)
	}
	return nil
}

// ensureFile creates the project dir and writes the session header if needed.
func (s *Session) ensureFile() error {
	if s.onDisk {
		return nil
	}
	if _, err := os.Stat(s.Path); err == nil {
		s.onDisk = true
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat session: %w", err)
	}
	if err := paths.EnsureHome(); err != nil {
		return fmt.Errorf("create zeta home: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	if err := s.writeEvent(event{
		Type:    typeSession,
		ID:      s.ID,
		Created: s.Created,
	}); err != nil {
		return err
	}
	s.onDisk = true
	return nil
}

func (s *Session) writeEvent(evt event) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal session event: %w", err)
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("append session: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("append session: %w", err)
	}
	return nil
}

func load(abs, path string) (*Session, []Record, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open session: %w", err)
	}
	defer f.Close()

	s := &Session{
		ID:     strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Path:   path,
		Cwd:    abs,
		onDisk: true,
	}
	var out []Record
	sc := bufio.NewScanner(f)
	// data: image URLs can be large (base64).
	sc.Buffer(make([]byte, 0, 64*1024), image.MaxJSONLLine)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var evt event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			return nil, nil, fmt.Errorf("session %s:%d: %w", filepath.Base(path), lineNo, err)
		}
		switch evt.Type {
		case typeSession:
			if evt.ID != "" {
				s.ID = evt.ID
			}
			if evt.Created != "" {
				s.Created = evt.Created
			}
		case typeMessage:
			out = append(out, Record{
				Role:       evt.Role,
				Text:       evt.Text,
				TS:         evt.TS,
				Images:     evt.Images,
				ToolCallID: evt.ToolCallID,
				ToolCalls:  evt.ToolCalls,
				Label:      evt.Label,
				Tool:       evt.Tool,
				Denied:     evt.Denied,
				Tail:       evt.Tail,
				Usage:      evt.Usage,
				Model:      evt.Model,
				FramePlan:  evt.FramePlan,
			})
		default:
			// Pre-1.0: skip deleted event types (e.g. old todos snapshots).
			continue
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("read session: %w", err)
	}
	if name, err := s.IndexedName(); err == nil {
		s.Name = name
	}
	return s, out, nil
}

func projectDirPath(abs string) string {
	return filepath.Join(paths.Home(), "sessions", CwdKey(abs))
}

// CwdKey encodes an absolute path into a filesystem-safe project key.
func CwdKey(abs string) string {
	var b strings.Builder
	for _, r := range abs {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	key := b.String()
	key = strings.Trim(key, "-")
	for strings.Contains(key, "--") {
		key = strings.ReplaceAll(key, "--", "-")
	}
	if key == "" {
		return "default"
	}
	return key
}

func absCwd(cwd string) (string, error) {
	if cwd == "" {
		cwd = "."
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}
	return abs, nil
}
