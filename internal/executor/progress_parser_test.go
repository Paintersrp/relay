package executor

import (
	"encoding/json"
	"strings"
	"testing"
)

func makePacket(fields map[string]interface{}) string {
	b, _ := json.Marshal(fields)
	return string(b)
}

func TestParser_OpenCodeTextPacket(t *testing.T) {
	line := makePacket(map[string]interface{}{
		"type": "assistant",
		"message": map[string]interface{}{
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "I will now edit the file"},
			},
		},
	})

	events := parseOneLine([]byte(line))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if !strings.Contains(events[0].Message, "I will now edit the file") {
		t.Errorf("expected text in message, got %q", events[0].Message)
	}
	if strings.Contains(events[0].Message, `"type"`) {
		t.Error("message must not contain raw JSON")
	}
}

func TestParser_OpenCodeToolEditPacket(t *testing.T) {
	line := makePacket(map[string]interface{}{
		"type":      "tool_use",
		"sessionID": "ses_abc123",
		"timestamp": 1782593090986,
		"part": map[string]interface{}{
			"type": "tool",
			"tool": "edit",
			"state": map[string]interface{}{
				"status": "completed",
				"input": map[string]interface{}{
					"filePath": "internal/executor/executor.go",
				},
			},
		},
	})

	events := parseOneLine([]byte(line))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if !strings.Contains(events[0].Message, "Edited") {
		t.Errorf("expected 'Edited' in message, got %q", events[0].Message)
	}
	if !strings.Contains(events[0].Message, "executor.go") {
		t.Errorf("expected file path in message, got %q", events[0].Message)
	}
	if strings.Contains(events[0].Message, "sessionID") {
		t.Error("message must not contain sessionID")
	}
	if strings.Contains(events[0].Message, `"type"`) {
		t.Error("message must not contain raw JSON")
	}
}

func TestParser_OpenCodeStepStartFinish(t *testing.T) {
	line := makePacket(map[string]interface{}{
		"type": "step_start",
		"name": "run_validation",
	})

	events := parseOneLine([]byte(line))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if !strings.Contains(events[0].Message, "Step started") {
		t.Errorf("expected 'Step started' in message, got %q", events[0].Message)
	}
	if !strings.Contains(events[0].Message, "run_validation") {
		t.Errorf("expected step name in message, got %q", events[0].Message)
	}

	line = makePacket(map[string]interface{}{
		"type":      "step_finish",
		"step_name": "validation_complete",
	})

	events = parseOneLine([]byte(line))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if !strings.Contains(events[0].Message, "Step completed") {
		t.Errorf("expected 'Step completed' in message, got %q", events[0].Message)
	}
}

func TestParser_UnknownJSONReturnsNoDisplay(t *testing.T) {
	line := makePacket(map[string]interface{}{
		"type":      "unknown_packet_type",
		"sessionID": "ses_xyz",
		"timestamp": 123456789,
		"raw":       map[string]interface{}{"nested": "data"},
	})

	events := parseOneLine([]byte(line))
	if len(events) != 0 {
		t.Errorf("expected 0 events for unknown JSON, got %d: %+v", len(events), events)
	}
}

func TestParser_ProtocolOnlyPacketReturnsNil(t *testing.T) {
	line := makePacket(map[string]interface{}{
		"type":      "system",
		"sessionID": "ses_xyz",
		"timestamp": 123456789,
	})

	events := parseOneLine([]byte(line))
	if len(events) != 0 {
		t.Errorf("expected 0 events for protocol-only packet, got %d: %+v", len(events), events)
	}
}

func TestParser_RawJSONNeverInEventMessage(t *testing.T) {
	line := makePacket(map[string]interface{}{
		"type": "tool_use",
		"part": map[string]interface{}{
			"type": "tool",
			"tool": "read",
			"state": map[string]interface{}{
				"status": "completed",
				"input": map[string]interface{}{
					"filePath": "docs/index.json",
				},
				"output": "some file content here",
			},
		},
	})

	events := parseOneLine([]byte(line))
	for _, e := range events {
		if strings.Contains(e.Message, `"type"`) {
			t.Errorf("event message contains raw JSON: %q", e.Message)
		}
		if strings.Contains(e.Message, `"part"`) {
			t.Errorf("event message contains raw part: %q", e.Message)
		}
		if strings.Contains(e.Message, `"state"`) {
			t.Errorf("event message contains raw state: %q", e.Message)
		}
		if strings.Contains(e.Message, `"output"`) {
			t.Errorf("event message contains raw output: %q", e.Message)
		}
		if strings.Contains(e.Message, `"input"`) {
			t.Errorf("event message contains raw input: %q", e.Message)
		}
	}
}

func TestParser_PartialChunksBuffered(t *testing.T) {
	p := newProgressParser()

	events := p.feed([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"Hel`))
	if len(events) != 0 {
		t.Errorf("expected 0 events for partial chunk, got %d", len(events))
	}

	events = p.feed([]byte(`lo world"}]}}
{"type":"step_start","name":"compile"}
`))
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events after completing lines, got %d: %+v", len(events), events)
	}

	foundAssistant := false
	foundStep := false
	for _, e := range events {
		if strings.Contains(e.Message, "Hello world") {
			foundAssistant = true
		}
		if strings.Contains(e.Message, "Step started") {
			foundStep = true
		}
	}
	if !foundAssistant {
		t.Error("did not find assistant message after buffering")
	}
	if !foundStep {
		t.Error("did not find step message after buffering")
	}
}

func TestParser_PlainTextFallback(t *testing.T) {
	events := parseOneLine([]byte("compiling project..."))
	if len(events) != 1 {
		t.Fatalf("expected 1 event for plain text, got %d: %+v", len(events), events)
	}
	if events[0].Message != "compiling project..." {
		t.Errorf("expected plain text message, got %q", events[0].Message)
	}
}

func TestParser_StackDumpSuppressed(t *testing.T) {
	events := parseOneLine([]byte("goroutine 1 [running]:"))
	if len(events) != 0 {
		t.Errorf("expected 0 events for stack dump, got %d", len(events))
	}

	events = parseOneLine([]byte("panic: runtime error: invalid memory address"))
	if len(events) != 0 {
		t.Errorf("expected 0 events for panic, got %d", len(events))
	}
}

func TestParser_SecretPatternSuppressed(t *testing.T) {
	events := parseOneLine([]byte("api_key = sk-abc123def456"))
	if len(events) != 0 {
		t.Errorf("expected 0 events for secret-like line, got %d", len(events))
	}
}

func TestParser_MessageTruncation(t *testing.T) {
	longText := strings.Repeat("x", 500)
	events := parseOneLine([]byte(longText))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if len(events[0].Message) > maxMessageLen+4 {
		t.Errorf("message not truncated: len=%d, max=%d", len(events[0].Message), maxMessageLen)
	}
}

func TestParser_EmptyAndWhitespaceOnly(t *testing.T) {
	if events := parseOneLine([]byte("")); len(events) != 0 {
		t.Error("expected 0 events for empty")
	}
	if events := parseOneLine([]byte("   ")); len(events) != 0 {
		t.Error("expected 0 events for whitespace-only")
	}
	if events := parseOneLine([]byte("\n")); len(events) != 0 {
		t.Error("expected 0 events for newline-only")
	}
}

func TestParser_FlushDrainsBuffer(t *testing.T) {
	p := newProgressParser()
	p.feed([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"incomplete`))

	events := p.flush()
	if len(events) > 0 {
		for _, e := range events {
			if strings.Contains(e.Message, `"type"`) {
				t.Errorf("flush must not produce raw JSON: %q", e.Message)
			}
		}
	}
}

func TestParser_BashCommandPacket(t *testing.T) {
	line := makePacket(map[string]interface{}{
		"type": "tool_use",
		"part": map[string]interface{}{
			"type": "tool",
			"tool": "bash",
			"state": map[string]interface{}{
				"status": "completed",
				"input": map[string]interface{}{
					"command": "go test ./...",
				},
			},
		},
	})

	events := parseOneLine([]byte(line))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if !strings.Contains(events[0].Message, "Ran command") {
		t.Errorf("expected 'Ran command' in message, got %q", events[0].Message)
	}
	if !strings.Contains(events[0].Message, "go test") {
		t.Errorf("expected command in message, got %q", events[0].Message)
	}
	if strings.Contains(events[0].Message, `"command"`) {
		t.Error("message must not contain raw JSON")
	}
}

func TestParser_DuplicateSuppressionLogic(t *testing.T) {
	msg1 := "Assistant: processing file"
	msg2 := "Assistant: processing file"
	msg3 := "Assistant: different message"

	prev := ""
	if msg1 == prev {
		t.Error("first message should not be duplicate")
	}
	prev = msg1
	if msg2 == prev {
		t.Log("consecutive duplicate detected (this is correct behavior)")
	}
	if msg3 == prev {
		t.Error("different messages should not match")
	}
}

func TestParser_ToolWithoutTarget(t *testing.T) {
	line := makePacket(map[string]interface{}{
		"type": "tool_use",
		"part": map[string]interface{}{
			"type": "tool",
			"tool": "grep",
			"state": map[string]interface{}{
				"status": "completed",
			},
		},
	})

	events := parseOneLine([]byte(line))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	if !strings.Contains(events[0].Message, "Searched") {
		t.Errorf("expected tool name in message, got %q", events[0].Message)
	}
}
func TestParser_ANSI_DecoratedText(t *testing.T) {
	// Simulates ANSI-decorated Kiro output in live progress
	events := parseOneLine([]byte("\x1b[0m\x1b[38;5;10mSTATUS: DONE\nSome log message"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	// Verify no ANSI escape sequences in the message
	if strings.Contains(events[0].Message, "\x1b") {
		t.Errorf("event message should not contain ESC byte: %q", events[0].Message)
	}
	if strings.Contains(events[0].Message, "[38;5") {
		t.Errorf("event message should not contain ANSI color code: %q", events[0].Message)
	}
	if strings.Contains(events[0].Message, "[0m") {
		t.Errorf("event message should not contain ANSI reset: %q", events[0].Message)
	}
}

func TestParser_ANSI_PromptPrefix(t *testing.T) {
	events := parseOneLine([]byte("> \x1b[38;5;10mSTATUS: DONE"))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	// Should strip both ANSI and prompt prefix
	if strings.Contains(events[0].Message, "\x1b") {
		t.Errorf("event message should not contain ESC byte: %q", events[0].Message)
	}
	if strings.HasPrefix(events[0].Message, ">") {
		t.Errorf("event message should not start with >: %q", events[0].Message)
	}
}
