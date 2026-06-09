package pipeline

import "testing"

func TestParseRecommendedModelExactLabel(t *testing.T) {
	text := "# Task\n\nRecommended Model: DeepSeek V4 Pro\n"
	got, ok := ParseRecommendedModel(text)
	if !ok {
		t.Fatal("expected ok = true")
	}
	if got != "DeepSeek V4 Pro" {
		t.Fatalf("expected 'DeepSeek V4 Pro', got %q", got)
	}
}

func TestParseMarkdownBoldRecommendedModel(t *testing.T) {
	text := "**Recommended Model:** qwen max"
	got, ok := ParseRecommendedModel(text)
	if !ok {
		t.Fatal("expected ok = true")
	}
	if got != "Qwen3.7 Max" {
		t.Fatalf("expected 'Qwen3.7 Max', got %q", got)
	}
}

func TestParseModelKeyAlias(t *testing.T) {
	text := "Model: deepseek pro max"
	got, ok := ParseRecommendedModel(text)
	if !ok {
		t.Fatal("expected ok = true")
	}
	if got != "DeepSeek V4 Pro Max" {
		t.Fatalf("expected 'DeepSeek V4 Pro Max', got %q", got)
	}
}

func TestParseNoModelMarkerReturnsFalse(t *testing.T) {
	text := "Use DeepSeek sometime maybe"
	got, ok := ParseRecommendedModel(text)
	if ok {
		t.Fatal("expected ok = false")
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestResolveDefaultModel(t *testing.T) {
	selected, source := ResolveSelectedModel("", "", "")
	if selected != "DeepSeek V4 Flash" {
		t.Fatalf("expected 'DeepSeek V4 Flash', got %q", selected)
	}
	if source != "default" {
		t.Fatalf("expected source 'default', got %q", source)
	}
}

func TestResolveParsedModelWithoutOverride(t *testing.T) {
	selected, source := ResolveSelectedModel("", "", "DeepSeek V4 Pro")
	if selected != "DeepSeek V4 Pro" {
		t.Fatalf("expected 'DeepSeek V4 Pro', got %q", selected)
	}
	if source != "parsed" {
		t.Fatalf("expected source 'parsed', got %q", source)
	}
}

func TestDropdownOverrideWinsOverParsed(t *testing.T) {
	selected, source := ResolveSelectedModel("qwen-3.7-max", "", "DeepSeek V4 Pro")
	if selected != "Qwen3.7 Max" {
		t.Fatalf("expected 'Qwen3.7 Max', got %q", selected)
	}
	if source != "override" {
		t.Fatalf("expected source 'override', got %q", source)
	}
}

func TestCustomOverrideWinsWhenCustomTextIsProvided(t *testing.T) {
	selected, source := ResolveSelectedModel("custom", "provider/model-id", "DeepSeek V4 Pro")
	if selected != "provider/model-id" {
		t.Fatalf("expected 'provider/model-id', got %q", selected)
	}
	if source != "custom" {
		t.Fatalf("expected source 'custom', got %q", source)
	}
}

func TestModelLabelForID(t *testing.T) {
	label, ok := ModelLabelForID("deepseek-v4-pro")
	if !ok {
		t.Fatal("expected ok = true")
	}
	if label != "DeepSeek V4 Pro" {
		t.Fatalf("expected 'DeepSeek V4 Pro', got %q", label)
	}
}

func TestModelLabelForIDEmpty(t *testing.T) {
	_, ok := ModelLabelForID("")
	if ok {
		t.Fatal("expected ok = false")
	}
}

func TestParseRecommendedModelFromExecutionModelUse(t *testing.T) {
	text := "## Execution model\n\nUse: DeepSeek V4 Flash\n"
	got, ok := ParseRecommendedModel(text)
	if !ok {
		t.Fatal("expected ok = true")
	}
	if got != "DeepSeek V4 Flash" {
		t.Fatalf("expected 'DeepSeek V4 Flash', got %q", got)
	}
}

func TestParseRecommendedModelFromExecutionModelUseMultipleModels(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"## Execution model\n\nUse: Qwen3.7 Max\n", "Qwen3.7 Max"},
		{"## Execution model\n\nUse: Kimi K2.6\n", "Kimi K2.6"},
		{"## Execution model\n\nUse: GPT-5.5 Thinking\n", "GPT-5.5 Thinking"},
	}
	for _, tc := range tests {
		got, ok := ParseRecommendedModel(tc.input)
		if !ok {
			t.Fatalf("expected ok = true for %q", tc.input)
		}
		if got != tc.expected {
			t.Fatalf("expected %q, got %q for input %q", tc.expected, got, tc.input)
		}
	}
}

func TestParseRecommendedModelUseOutsideExecModelSection(t *testing.T) {
	text := "Use: DeepSeek V4 Flash\n"
	got, ok := ParseRecommendedModel(text)
	if ok {
		t.Fatalf("expected ok = false for bare Use: outside ## Execution model, got %q", got)
	}
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestNormalizeModelLabel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		ok       bool
	}{
		{"deepseek flash", "DeepSeek V4 Flash", true},
		{"DeepSeek V4 Flash", "DeepSeek V4 Flash", true},
		{"deepseek-v4-flash", "DeepSeek V4 Flash", true},
		{"deepseek pro", "DeepSeek V4 Pro", true},
		{"custom-model", "custom-model", true},
		{"", "", false},
	}
	for _, tc := range tests {
		got, ok := NormalizeModelLabel(tc.input)
		if ok != tc.ok {
			t.Errorf("NormalizeModelLabel(%q): expected ok=%v, got %v", tc.input, tc.ok, ok)
		}
		if got != tc.expected {
			t.Errorf("NormalizeModelLabel(%q): expected %q, got %q", tc.input, tc.expected, got)
		}
	}
}

func TestParseRecommendedModelMarkdownBullet(t *testing.T) {
	text := "- Recommended Model: Kimi K2.6"
	got, ok := ParseRecommendedModel(text)
	if !ok {
		t.Fatal("expected ok = true")
	}
	if got != "Kimi K2.6" {
		t.Fatalf("expected 'Kimi K2.6', got %q", got)
	}
}

func TestParseRecommendedModelUseModelKey(t *testing.T) {
	text := "Use model: gpt-5.5-thinking"
	got, ok := ParseRecommendedModel(text)
	if !ok {
		t.Fatal("expected ok = true")
	}
	if got != "GPT-5.5 Thinking" {
		t.Fatalf("expected 'GPT-5.5 Thinking', got %q", got)
	}
}

func TestParseRecommendedModelSuggestedModel(t *testing.T) {
	text := "Suggested Model: qwen max"
	got, ok := ParseRecommendedModel(text)
	if !ok {
		t.Fatal("expected ok = true")
	}
	if got != "Qwen3.7 Max" {
		t.Fatalf("expected 'Qwen3.7 Max', got %q", got)
	}
}

func TestResolveCustomWithoutCustomTextFallsBackToParsed(t *testing.T) {
	selected, source := ResolveSelectedModel("custom", "", "DeepSeek V4 Pro")
	if selected != "DeepSeek V4 Pro" {
		t.Fatalf("expected 'DeepSeek V4 Pro', got %q", selected)
	}
	if source != "parsed" {
		t.Fatalf("expected source 'parsed', got %q", source)
	}
}

func TestResolveCustomWithoutAnythingDefaults(t *testing.T) {
	selected, source := ResolveSelectedModel("custom", "", "")
	if selected != "DeepSeek V4 Flash" {
		t.Fatalf("expected 'DeepSeek V4 Flash', got %q", selected)
	}
	if source != "default" {
		t.Fatalf("expected source 'default', got %q", source)
	}
}

func TestModelOptionsCount(t *testing.T) {
	opts := ModelOptions()
	if len(opts) != 7 {
		t.Fatalf("expected 7 options, got %d", len(opts))
	}
}

func TestModelOptionsFirstIsDefault(t *testing.T) {
	opts := ModelOptions()
	if !opts[0].IsDefault {
		t.Fatal("expected first option to be default")
	}
}

func TestModelOptionsLastIsCustom(t *testing.T) {
	opts := ModelOptions()
	if !opts[len(opts)-1].IsCustom {
		t.Fatal("expected last option to be custom")
	}
}
