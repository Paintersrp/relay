package executor

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

type ExecutorProgressEvent struct {
	Level   string
	Message string
	Kind    string
}

type progressParser struct {
	buf bytes.Buffer
}

func newProgressParser() *progressParser {
	return &progressParser{}
}

var (
	maxMessageLen      = 240
	secretPattern      = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|auth)\s*[:=]\s*\S+`)
	stackDumpPattern   = regexp.MustCompile(`^\s*(goroutine \d+|panic:|\[(?:Ff)atal\]|runtime error|signal)`)
	jsonObjLinePattern = regexp.MustCompile(`^\s*\{`)
)

// stripANSI removes ANSI terminal control sequences from text.
func stripANSI(text string) string {
	text = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`).ReplaceAllString(text, "")
	text = regexp.MustCompile(`\x1b\][0-9:;]*[^\x07\x1b]`).ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\x1b", "")
	return text
}

func (p *progressParser) feed(chunk []byte) []ExecutorProgressEvent {
	p.buf.Write(chunk)
	return p.drainCompleteLines()
}

func (p *progressParser) flush() []ExecutorProgressEvent {
	remainder := p.buf.Bytes()
	p.buf.Reset()
	trimmed := bytes.TrimSpace(remainder)
	if len(trimmed) == 0 {
		return nil
	}
	parsed := parseOneLine(trimmed)
	var clean []ExecutorProgressEvent
	for _, ev := range parsed {
		if !strings.Contains(ev.Message, `"type"`) && !strings.Contains(ev.Message, `{`) {
			clean = append(clean, ev)
		}
	}
	return clean
}

func (p *progressParser) drainCompleteLines() []ExecutorProgressEvent {
	data := p.buf.Bytes()
	idx := bytes.LastIndexByte(data, '\n')
	if idx < 0 {
		return nil
	}

	complete := data[:idx]
	incomplete := data[idx+1:]

	p.buf.Reset()
	if len(incomplete) > 0 {
		p.buf.Write(incomplete)
	}

	var events []ExecutorProgressEvent
	for _, line := range bytes.Split(complete, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		line = bytes.TrimRight(line, "\r")
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		parsed := parseOneLine(line)
		events = append(events, parsed...)
	}

	return events
}

func parseOneLine(line []byte) []ExecutorProgressEvent {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 {
		return nil
	}

	// Strip ANSI control sequences before processing
	text := stripANSI(string(trimmed))
	if text == "" {
		return nil
	}

	if jsonObjLinePattern.Match([]byte(text)) {
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(text), &parsed); err == nil {
			return parseOpenCodePacket(parsed)
		}
	}

	if text == "" {
		return nil
	}

	if stackDumpPattern.MatchString(text) {
		return nil
	}

	if secretPattern.MatchString(text) {
		return nil
	}

	text = collapseWhitespace(text)
	if text == "" {
		return nil
	}

	text = truncateMessage(text)

	return []ExecutorProgressEvent{
		{Level: "info", Message: text, Kind: "executor_text"},
	}
}

func parseOpenCodePacket(packet map[string]interface{}) []ExecutorProgressEvent {
	if len(packet) == 0 {
		return nil
	}

	if isProtocolOnly(packet) {
		return nil
	}

	pktType, _ := packet["type"].(string)

	switch pktType {
	case "assistant":
		return parseAssistantPacket(packet)
	case "user":
		return parseTextPacket(packet, "User")
	case "tool_use":
		return parseToolUsePacket(packet)
	case "tool_result":
		return parseToolResultPacket(packet)
	case "step_start":
		return parseStepPacket(packet, "started")
	case "step_finish":
		return parseStepPacket(packet, "completed")
	}

	part, _ := packet["part"].(map[string]interface{})
	if part != nil {
		partType, _ := part["type"].(string)
		switch partType {
		case "text":
			return parseTextPacket(packet, "Assistant")
		case "tool":
			return parseToolUsePacket(packet)
		case "reasoning":
			return parseTextPacket(packet, "Thinking")
		}
	}

	return nil
}

func isProtocolOnly(packet map[string]interface{}) bool {
	meaningful := false
	for k := range packet {
		if k != "type" && k != "sessionID" && k != "timestamp" && k != "id" &&
			k != "parentID" && k != "callID" && k != "uuid" {
			meaningful = true
			break
		}
	}
	return !meaningful
}

func parseAssistantPacket(packet map[string]interface{}) []ExecutorProgressEvent {
	msg, _ := packet["message"].(map[string]interface{})
	if msg == nil {
		return nil
	}
	content, _ := msg["content"].([]interface{})
	if len(content) == 0 {
		return nil
	}

	var texts []string
	for _, c := range content {
		cm, _ := c.(map[string]interface{})
		if cm == nil {
			continue
		}
		ct, _ := cm["type"].(string)
		txt, _ := cm["text"].(string)
		if ct == "text" && txt != "" {
			txt = collapseWhitespace(txt)
			if txt != "" {
				texts = append(texts, txt)
			}
		}
	}

	if len(texts) == 0 {
		return nil
	}

	combined := strings.Join(texts, " ")
	combined = truncateMessage(combined)
	if combined == "" {
		return nil
	}

	return []ExecutorProgressEvent{
		{Level: "info", Message: "Assistant: " + combined, Kind: "assistant_text"},
	}
}

func parseTextPacket(packet map[string]interface{}, prefix string) []ExecutorProgressEvent {
	text := extractText(packet)
	if text == "" {
		return nil
	}
	text = collapseWhitespace(text)
	text = truncateMessage(text)
	if text == "" {
		return nil
	}
	return []ExecutorProgressEvent{
		{Level: "info", Message: prefix + ": " + text, Kind: "text"},
	}
}

func extractText(packet map[string]interface{}) string {
	if text, ok := packet["text"].(string); ok && text != "" {
		return text
	}

	msg, _ := packet["message"].(map[string]interface{})
	if msg != nil {
		if text, ok := msg["text"].(string); ok && text != "" {
			return text
		}
		content, _ := msg["content"].([]interface{})
		if len(content) > 0 {
			for _, c := range content {
				cm, _ := c.(map[string]interface{})
				if cm == nil {
					continue
				}
				ct, _ := cm["type"].(string)
				txt, _ := cm["text"].(string)
				if (ct == "text" || ct == "") && txt != "" {
					return txt
				}
			}
		}
	}

	part, _ := packet["part"].(map[string]interface{})
	if part != nil {
		if text, ok := part["text"].(string); ok && text != "" {
			return text
		}
		if reasoning, ok := part["reasoning"].(string); ok && reasoning != "" {
			return reasoning
		}
	}

	return ""
}

func parseToolUsePacket(packet map[string]interface{}) []ExecutorProgressEvent {
	tool, target, status := extractToolInfo(packet)
	if tool == "" {
		return nil
	}

	msg := formatToolMessage(tool, target, status)
	if msg == "" {
		return nil
	}
	msg = truncateMessage(msg)

	return []ExecutorProgressEvent{
		{Level: "info", Message: msg, Kind: "tool"},
	}
}

func extractToolInfo(packet map[string]interface{}) (tool, target, status string) {
	part, _ := packet["part"].(map[string]interface{})

	if part != nil {
		tool, _ = part["tool"].(string)
		if tool == "" {
			tool, _ = part["type"].(string)
		}
		state, _ := part["state"].(map[string]interface{})
		if state != nil {
			status, _ = state["status"].(string)
			input, _ := state["input"].(map[string]interface{})
			if input != nil {
				target = extractTarget(input)
			}
		}
	}

	if tool == "" {
		tool, _ = packet["tool"].(string)
	}

	if status == "" {
		state, _ := packet["state"].(map[string]interface{})
		if state != nil {
			status, _ = state["status"].(string)
			input, _ := state["input"].(map[string]interface{})
			if input != nil {
				target = extractTarget(input)
			}
		}
	}

	if tool == "" {
		pktType, _ := packet["type"].(string)
		tool = pktType
	}

	return
}

func extractTarget(input map[string]interface{}) string {
	if fp, ok := input["filePath"].(string); ok && fp != "" {
		path := strings.ReplaceAll(fp, "\\", "/")
		if idx := strings.LastIndex(path, "/relay/"); idx >= 0 {
			path = path[idx+len("/relay/"):]
		}
		return path
	}
	if cmd, ok := input["command"].(string); ok && cmd != "" {
		return cmd
	}
	return ""
}

func formatToolMessage(tool, target, status string) string {
	var b strings.Builder

	switch tool {
	case "read":
		b.WriteString("Read file")
	case "write":
		b.WriteString("Wrote file")
	case "edit":
		b.WriteString("Edited")
	case "bash", "shell", "run_shell_command":
		b.WriteString("Ran command")
	case "grep", "search", "glob":
		b.WriteString("Searched")
	case "webfetch", "fetch":
		b.WriteString("Fetched URL")
	default:
		b.WriteString(tool)
	}

	if target != "" && isEditTool(tool) {
		b.WriteString(" ")
		b.WriteString(target)
	} else if target != "" && isShellTool(tool) {
		b.WriteString(": ")
		b.WriteString(truncateMessage(target))
	} else if target != "" {
		b.WriteString(" ")
		b.WriteString(target)
	}

	if status != "" && status != "completed" {
		b.WriteString(" (")
		b.WriteString(status)
		b.WriteString(")")
	}

	return b.String()
}

func isEditTool(tool string) bool {
	switch tool {
	case "edit", "write":
		return true
	}
	return false
}

func isShellTool(tool string) bool {
	switch tool {
	case "bash", "shell", "run_shell_command":
		return true
	}
	return false
}

func parseToolResultPacket(packet map[string]interface{}) []ExecutorProgressEvent {
	tool, _ := packet["tool"].(string)
	if tool == "" {
		return nil
	}
	msg := "Tool result: " + tool
	msg = truncateMessage(msg)
	return []ExecutorProgressEvent{
		{Level: "info", Message: msg, Kind: "tool_result"},
	}
}

func parseStepPacket(packet map[string]interface{}, action string) []ExecutorProgressEvent {
	label := ""
	if name, ok := packet["name"].(string); ok && name != "" {
		label = name
	} else if stepName, ok := packet["step_name"].(string); ok && stepName != "" {
		label = stepName
	} else if title, ok := packet["title"].(string); ok && title != "" {
		label = title
	}
	if label == "" {
		label = "step"
	}
	msg := "Step " + action + ": " + label
	msg = truncateMessage(msg)
	return []ExecutorProgressEvent{
		{Level: "info", Message: msg, Kind: "step"},
	}
}

func collapseWhitespace(s string) string {
	s = strings.TrimSpace(s)
	parts := strings.Fields(s)
	return strings.Join(parts, " ")
}

func truncateMessage(s string) string {
	if len(s) <= maxMessageLen {
		return s
	}
	return s[:maxMessageLen] + "..."
}
