package instructions

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
)

//go:embed assets/surgical-chat-instructions.txt
var SurgicalChatInstructions string

//go:embed assets/AGENTS.md
var AssetsAGENTSMD string

//go:embed assets/.clinerules
var AssetsClineRules string

type Asset struct {
	Key         string
	Label       string
	Filename    string
	Content     string
	Source      string
	Description string
	ContentType string
}

func Registry() []Asset {
	return []Asset{
		{
			Key:         "surgical-chat-instructions",
			Label:       "Surgical Chat Instructions",
			Filename:    "surgical-chat-instructions.txt",
			Content:     SurgicalChatInstructions,
			Source:      "internal/instructions/assets/surgical-chat-instructions.txt",
			Description: "Canonical structure and rules for writing surgical implementation handoffs.",
			ContentType: "text/plain; charset=utf-8",
		},
		{
			Key:         "agents-md",
			Label:       "AGENTS.md",
			Filename:    "AGENTS.md",
			Content:     AssetsAGENTSMD,
			Source:      "internal/instructions/assets/AGENTS.md",
			Description: "Canonical agent instructions for working with Relay.",
			ContentType: "text/markdown; charset=utf-8",
		},
		{
			Key:         "clinerules",
			Label:       ".clinerules",
			Filename:    ".clinerules",
			Content:     AssetsClineRules,
			Source:      "internal/instructions/assets/.clinerules",
			Description: "Canonical Cline rules for working with this repository.",
			ContentType: "text/plain; charset=utf-8",
		},
	}
}

func FindAsset(key string) *Asset {
	for _, a := range Registry() {
		if a.Key == key {
			return &a
		}
	}
	return nil
}

func AssetDownloadHeaders(filename string) (string, string) {
	ext := strings.ToLower(filepath.Ext(filename))
	contentType := "text/plain; charset=utf-8"
	switch ext {
	case ".md", ".markdown":
		contentType = "text/markdown; charset=utf-8"
	}
	disposition := fmt.Sprintf(`attachment; filename="%s"`, filename)
	return contentType, disposition
}
