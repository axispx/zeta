package tui

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/axispx/zeta/internal/styles"
	"github.com/axispx/zeta/internal/tools"
)

const (
	maxGroupTools   = 3 // visible tool lines; earlier ones collapse under the header
	maxBashOutLines = 3 // live/final bash stdout lines shown under "$ cmd"
)

// toolView describes how a tool appears in the transcript and busy chrome.
// Tools that share the same segment key stack together; empty key = generic cluster.
type toolView struct {
	keepOut   bool                   // persist Message.Out from the tool result
	segment   string                 // non-empty → own segment kind (bash/edit/…)
	busy      string                 // chrome label while the tool runs; empty → statusWorking
	renderRun func([]Message) string // nil → renderToolCluster
}

func viewFor(name string) toolView {
	switch name {
	case tools.Bash:
		return toolView{keepOut: true, segment: tools.Bash, busy: statusRunning, renderRun: renderShellRun}
	case tools.Edit, tools.Write:
		return toolView{keepOut: true, segment: tools.Edit, busy: statusEditing, renderRun: renderEditRun}
	case tools.Todo:
		// keepOut so resume paints the model-facing Format body as the row.
		return toolView{keepOut: true, segment: tools.Todo, busy: statusWorking, renderRun: renderTodoRun}
	case tools.Read, tools.Skill:
		return toolView{busy: statusReading}
	case tools.Grep, tools.Glob, tools.WebSearch:
		return toolView{busy: statusSearching}
	case tools.WebFetch:
		return toolView{busy: statusFetching}
	case tools.AskUser:
		return toolView{keepOut: true, busy: statusWaiting}
	default:
		return toolView{}
	}
}

// toolHasOut is true when the tool row keeps Message.Out for the transcript UI.
func toolHasOut(name string) bool { return viewFor(name).keepOut }

// toolStatus is the busy chrome label for a tool name.
func toolStatus(name string) string {
	if s := viewFor(name).busy; s != "" {
		return s
	}
	return statusWorking
}

// toolRunAt returns consecutive tool messages starting at i, or nil.
func toolRunAt(msgs []Message, i int) []Message {
	if i >= len(msgs) || msgs[i].Role != RoleTool {
		return nil
	}
	end := i + 1
	for end < len(msgs) && msgs[end].Role == RoleTool {
		end++
	}
	return msgs[i:end]
}

// renderToolGroup collapses a consecutive tool run into compact blocks.
// Shell calls render as "$ cmd". Edits render as "Editing/Creating …" while
// open, then "Edited/Created/Wrote" once done, plus a colored diff.
// A blank line separates segment kinds; consecutive shell lines stack tightly.
func renderToolGroup(msgs []Message, width, topMargin int) string {
	var b strings.Builder
	for i, seg := range splitToolSegments(msgs) {
		if i > 0 {
			b.WriteString("\n\n")
		}
		v := viewFor(seg.msgs[0].Tool)
		if v.renderRun != nil {
			b.WriteString(v.renderRun(seg.msgs))
		} else {
			b.WriteString(renderToolCluster(seg.msgs))
		}
	}
	body := widthBody(b.String(), width)
	if topMargin > 0 {
		return lipgloss.NewStyle().MarginTop(topMargin).Render(body)
	}
	return body
}

type toolSegment struct {
	msgs []Message
}

// splitToolSegments splits a tool run into maximal runs of the same view segment.
func splitToolSegments(msgs []Message) []toolSegment {
	var out []toolSegment
	for i := 0; i < len(msgs); {
		key := viewFor(msgs[i].Tool).segment
		j := i + 1
		for j < len(msgs) && viewFor(msgs[j].Tool).segment == key {
			j++
		}
		out = append(out, toolSegment{msgs: msgs[i:j]})
		i = j
	}
	return out
}

func renderShellRun(msgs []Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(renderShellCall(m))
	}
	return b.String()
}

func renderEditRun(msgs []Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(renderEditCall(m))
	}
	return b.String()
}

func renderTodoRun(msgs []Message) string {
	// Last snapshot in a consecutive run is enough (full replace each call).
	if len(msgs) == 0 {
		return ""
	}
	return renderTodoCall(msgs[len(msgs)-1])
}

// renderTodoCall shows the model-facing Format body (no second UI dialect).
func renderTodoCall(m Message) string {
	if m.Status == ToolDenied {
		return styles.ToolMsg.Render(tools.Todo) + "  " + styles.SystemMsg.Render("denied")
	}
	body := strings.TrimSpace(m.Out)
	if body == "" {
		return styles.ToolMsg.Render(tools.Todo)
	}
	return styles.ToolMsg.Render(body)
}

// renderEditCall formats an edit as "Editing|Creating|…" while open, then
// "Edited|Created|Wrote" once done, plus a colored unified diff.
// Message.Out is the unified diff body (preview or tool result). Verb/path come from Message.Text.
func renderEditCall(m Message) string {
	verb, name := editLabel(m.Text, m.Status != ToolRunning)
	adds, dels, colored := formatUnifiedDiff(m.Out)

	var b strings.Builder
	b.WriteString(styles.Prompt.Render(verb))
	if name != "" && name != "." {
		b.WriteString("  ")
		b.WriteString(styles.DiffFile.Render(name))
	}
	if m.Status == ToolDenied {
		b.WriteString("  ")
		b.WriteString(styles.SystemMsg.Render("denied"))
		return b.String()
	}
	if adds > 0 {
		b.WriteString("  ")
		b.WriteString(styles.DiffAdd.Render("+" + strconv.Itoa(adds)))
	}
	if dels > 0 {
		b.WriteString("  ")
		b.WriteString(styles.DiffDel.Render("-" + strconv.Itoa(dels)))
	}
	if colored != "" {
		b.WriteByte('\n')
		b.WriteString(colored)
	}
	return b.String()
}

// editLabel derives the UI verb and basename from the tool Summary label.
// Progressive while the call is open; past tense after it finishes.
func editLabel(text string, done bool) (verb, name string) {
	head, path, _ := strings.Cut(strings.TrimSpace(text), " ")
	base := filepath.Base(path)
	switch head {
	case "create":
		if done {
			return "Created", base
		}
		return "Creating", base
	case tools.Write:
		if done {
			return "Wrote", base
		}
		return "Writing", base
	default:
		if done {
			return "Edited", base
		}
		return "Editing", base
	}
}

// renderShellCall formats a shell Summary as "$ cmd" plus the last few output lines.
// Successful exit status is omitted from the UI; errors/timeouts are kept.
func renderShellCall(m Message) string {
	cmd := strings.TrimSpace(strings.TrimPrefix(m.Text, m.Tool))
	line := "$"
	if cmd != "" {
		line = "$ " + cmd
	}
	if m.Status == ToolDenied {
		return line + "  " + styles.SystemMsg.Render("denied")
	}
	tail := lastNonEmptyLines(stripOKExit(m.Out), maxBashOutLines)
	if tail == "" {
		return line
	}
	var b strings.Builder
	b.WriteString(line)
	for _, l := range strings.Split(tail, "\n") {
		b.WriteByte('\n')
		b.WriteString(styles.ToolMsg.Render(l))
	}
	return b.String()
}

// stripOKExit removes a trailing successful exit status from bash tool output.
// Errors and timeouts are left intact for the UI.
func stripOKExit(s string) string {
	if s == "exit: 0" {
		return ""
	}
	if before, ok := strings.CutSuffix(s, "\nexit: 0"); ok {
		return before
	}
	return s
}

// lastNonEmptyLines returns the last n non-empty lines of s.
func lastNonEmptyLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" || n <= 0 {
		return ""
	}
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// renderToolCluster renders one non-rich tool group: verb header, hidden
// earlier lines, then the visible tail. The verb header is always shown,
// including for a single tool call.
func renderToolCluster(msgs []Message) string {
	n := len(msgs)
	var b strings.Builder
	names := make([]string, n)
	for i, m := range msgs {
		names[i] = m.Tool
	}
	verbs, counts := toolGroupHeader(names)
	b.WriteString(styles.Prompt.Render(verbs))
	b.WriteString("  ")
	b.WriteString(styles.SystemMsg.Render(counts))
	start := 0
	if n > maxGroupTools {
		start = n - maxGroupTools
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(styles.ToolMsg.Render("... " + strconv.Itoa(start) + " earlier items hidden"))
	}
	for i := start; i < n; i++ {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(styles.ToolMsg.Render(msgs[i].Text))
	}
	return b.String()
}

type toolGroupMeta struct {
	verb string // title case, e.g. "Grepped"
	one  string // singular count noun
	many string // plural count noun
}

func metaForTool(name string) toolGroupMeta {
	switch name {
	case tools.Grep:
		return toolGroupMeta{verb: "Grepped", one: tools.Grep, many: "greps"}
	case tools.Glob:
		return toolGroupMeta{verb: "Globbed", one: tools.Glob, many: "globs"}
	case tools.Read:
		return toolGroupMeta{verb: "Read", one: "file", many: "files"}
	case tools.Skill:
		return toolGroupMeta{verb: "Skill", one: tools.Skill, many: "skills"}
	case "":
		return toolGroupMeta{verb: "Used", one: "tool", many: "tools"}
	default:
		// Use the bare tool name for both counts — auto-plural "bashs" is worse
		// than invariant "2 bash". Known tools above have proper plurals.
		verb := strings.ToUpper(name[:1]) + name[1:]
		return toolGroupMeta{verb: verb, one: name, many: name}
	}
}

// toolGroupHeader builds "Grepped, read" and "2 greps, 1 file" from tool names.
// Order is descending frequency, then name.
func toolGroupHeader(names []string) (verbs, counts string) {
	freq := map[string]int{}
	for _, n := range names {
		freq[n]++
	}
	keys := make([]string, 0, len(freq))
	for k := range freq {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if freq[keys[i]] != freq[keys[j]] {
			return freq[keys[i]] > freq[keys[j]]
		}
		return keys[i] < keys[j]
	})

	verbParts := make([]string, 0, len(keys))
	countParts := make([]string, 0, len(keys))
	for i, k := range keys {
		m := metaForTool(k)
		verb := m.verb
		if i > 0 {
			verb = strings.ToLower(verb)
		}
		verbParts = append(verbParts, verb)
		n := freq[k]
		noun := m.many
		if n == 1 {
			noun = m.one
		}
		countParts = append(countParts, strconv.Itoa(n)+" "+noun)
	}
	return strings.Join(verbParts, ", "), strings.Join(countParts, ", ")
}
