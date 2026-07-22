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

	"github.com/axispx/zeta/internal/paths"
)

// Roles stored in message events.
const (
	RoleUser  = "user"
	RoleAgent = "agent"
	RoleError = "error"
	RoleTool  = "tool"
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

// Record is a chat turn loaded from the transcript (message events only).
type Record struct {
	Role       string     `json:"role"`
	Text       string     `json:"text"`
	TS         string     `json:"ts"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Label      string     `json:"label,omitempty"` // UI label for tool rows
}

// event is one JSONL line: session header or a message.
type event struct {
	Type       string     `json:"type"`
	ID         string     `json:"id,omitempty"`
	Created    string     `json:"created,omitempty"`
	Role       string     `json:"role,omitempty"`
	Text       string     `json:"text,omitempty"`
	TS         string     `json:"ts,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Label      string     `json:"label,omitempty"`
}

// Session is an append-only JSONL transcript for one chat.
// A new session is in-memory only until the first Append.
type Session struct {
	ID      string
	Path    string
	Cwd     string
	Created string
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
		ToolCallID: rec.ToolCallID,
		ToolCalls:  rec.ToolCalls,
		Label:      rec.Label,
	}); err != nil {
		return err
	}
	return s.upsertIndex()
}

// ensureFile creates the project dir and writes the session header if needed.
func (s *Session) ensureFile() error {
	if _, err := os.Stat(s.Path); err == nil {
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
	return s.writeEvent(event{
		Type:    typeSession,
		ID:      s.ID,
		Created: s.Created,
	})
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
		ID:   strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Path: path,
		Cwd:  abs,
	}
	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
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
				ToolCallID: evt.ToolCallID,
				ToolCalls:  evt.ToolCalls,
				Label:      evt.Label,
			})
		default:
			return nil, nil, fmt.Errorf("session %s:%d: unknown event type %q", filepath.Base(path), lineNo, evt.Type)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("read session: %w", err)
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
