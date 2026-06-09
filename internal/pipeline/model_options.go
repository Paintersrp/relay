package pipeline

import (
	"regexp"
	"strings"
)

type ModelOption struct {
	ID          string
	Label       string
	Provider    string
	Description string
	IsDefault   bool
	IsCustom    bool
}

const DefaultModelID = "deepseek-v4-flash"
const DefaultModelLabel = "DeepSeek V4 Flash"
const CustomModelID = "custom"

var modelOptions = []ModelOption{
	{ID: "deepseek-v4-flash", Label: "DeepSeek V4 Flash", Provider: "DeepSeek", Description: "", IsDefault: true},
	{ID: "deepseek-v4-pro", Label: "DeepSeek V4 Pro", Provider: "DeepSeek"},
	{ID: "deepseek-v4-pro-max", Label: "DeepSeek V4 Pro Max", Provider: "DeepSeek"},
	{ID: "qwen-3.7-max", Label: "Qwen3.7 Max", Provider: "Qwen"},
	{ID: "kimi-k2.6", Label: "Kimi K2.6", Provider: "Kimi"},
	{ID: "gpt-5.5-thinking", Label: "GPT-5.5 Thinking", Provider: "OpenAI"},
	{ID: "custom", Label: "Custom", Provider: "Custom", IsCustom: true},
}

func ModelOptions() []ModelOption {
	return modelOptions
}

func ModelLabelForID(id string) (string, bool) {
	for _, opt := range modelOptions {
		if opt.ID == id {
			return opt.Label, true
		}
	}
	return "", false
}

var aliasMap = map[string]string{
	"deepseek flash":      DefaultModelLabel,
	"deepseek v4 flash":   DefaultModelLabel,
	"deepseek-v4-flash":   DefaultModelLabel,
	"deepseek pro":        "DeepSeek V4 Pro",
	"deepseek v4 pro":     "DeepSeek V4 Pro",
	"deepseek-v4-pro":     "DeepSeek V4 Pro",
	"deepseek pro max":    "DeepSeek V4 Pro Max",
	"deepseek v4 pro max": "DeepSeek V4 Pro Max",
	"deepseek-v4-pro-max": "DeepSeek V4 Pro Max",
	"qwen max":            "Qwen3.7 Max",
	"qwen 3.7 max":        "Qwen3.7 Max",
	"qwen3.7 max":         "Qwen3.7 Max",
	"qwen-3.7-max":        "Qwen3.7 Max",
	"kimi":                "Kimi K2.6",
	"kimi k2.6":           "Kimi K2.6",
	"kimi-k2.6":           "Kimi K2.6",
	"gpt 5.5 thinking":    "GPT-5.5 Thinking",
	"gpt-5.5-thinking":    "GPT-5.5 Thinking",
}

func NormalizeModelLabel(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	lower := strings.ToLower(trimmed)
	if label, ok := aliasMap[lower]; ok {
		return label, true
	}
	return trimmed, true
}

var recommendedModelKeyRe = regexp.MustCompile(`(?i)^(?:\*\*)?(?:Recommended Model|Model|Use model|Suggested model)(?::\*\*|:\*|:|)\s*(.+)$`)
var executionModelUseRe = regexp.MustCompile(`(?i)^Use:\s*(.+)$`)

func ParseRecommendedModel(text string) (string, bool) {
	lines := strings.Split(text, "\n")

	// First pass: check for ## Execution model / Use: pattern
	inExecModel := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inExecModel {
			if strings.EqualFold(trimmed, "## Execution model") {
				inExecModel = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if m := executionModelUseRe.FindStringSubmatch(trimmed); len(m) > 1 {
			val := strings.TrimSpace(m[1])
			if val != "" {
				return NormalizeModelLabel(val)
			}
		}
	}

	// Second pass: global scan for existing labels
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Strip one leading markdown bullet prefix
		if len(trimmed) > 2 && (trimmed[0] == '-' || trimmed[0] == '*' || trimmed[0] == '+') && trimmed[1] == ' ' {
			trimmed = strings.TrimSpace(trimmed[2:])
		}
		// Strip markdown bold markers around the key
		trimmed = strings.TrimPrefix(trimmed, "**")
		if m := recommendedModelKeyRe.FindStringSubmatch(trimmed); len(m) > 1 {
			val := strings.TrimSpace(m[1])
			if val == "" {
				continue
			}
			return NormalizeModelLabel(val)
		}
	}
	return "", false
}

func ResolveSelectedModel(optionID string, customText string, parsedRecommended string) (selected string, source string) {
	customText = strings.TrimSpace(customText)
	parsedRecommended = strings.TrimSpace(parsedRecommended)

	if optionID == "custom" {
		if customText != "" {
			return customText, "custom"
		}
		if parsedRecommended != "" {
			return parsedRecommended, "parsed"
		}
		return DefaultModelLabel, "default"
	}

	if optionID != "" {
		if label, ok := ModelLabelForID(optionID); ok {
			return label, "override"
		}
		// unknown option ID, fall back
	}

	if parsedRecommended != "" {
		return parsedRecommended, "parsed"
	}

	return DefaultModelLabel, "default"
}
