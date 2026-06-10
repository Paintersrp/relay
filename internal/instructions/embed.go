package instructions

import _ "embed"

//go:embed handoff_instructions.md
var HandoffInstructions string

//go:embed agents_md.md
var AgentsMD string

//go:embed clinerules.md
var ClineRules string

//go:embed surgical_chat_instructions.txt
var SurgicalChatInstructionsLegacy string
