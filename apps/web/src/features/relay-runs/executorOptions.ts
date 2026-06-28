import type { RelayExecutorAdapter } from "./types";

export type RelayExecutorOption = {
  value: RelayExecutorAdapter;
  label: string;
  description: string;
};

export type RelayExecutorModelOption = {
  value: string;
  label: string;
};

export const EXECUTOR_ADAPTER_OPTIONS: RelayExecutorOption[] = [
  {
    value: "opencode_go",
    label: "OpenCode Go",
    description: "Run through the OpenCode Go executor adapter.",
  },
  {
    value: "codex",
    label: "Codex",
    description: "Run through the Codex executor adapter.",
  },
  {
    value: "antigravity",
    label: "Antigravity",
    description: "Run through the Antigravity executor adapter.",
  },
  {
    value: "kiro_cli",
    label: "Kiro CLI",
    description: "Run through the Kiro CLI executor adapter.",
  },
];

export const KIRO_MODEL_OPTIONS: RelayExecutorModelOption[] = [
  { value: "auto", label: "Auto" },
  { value: "claude-opus-4.8", label: "Claude Opus 4.8" },
  { value: "claude-opus-4.7", label: "Claude Opus 4.7" },
  { value: "claude-opus-4.6", label: "Claude Opus 4.6" },
  { value: "claude-sonnet-4.6", label: "Claude Sonnet 4.6" },
  { value: "claude-opus-4.5", label: "Claude Opus 4.5" },
  { value: "claude-sonnet-4.5", label: "Claude Sonnet 4.5" },
  { value: "claude-sonnet-4", label: "Claude Sonnet 4" },
  { value: "claude-haiku-4.5", label: "Claude Haiku 4.5" },
  { value: "deepseek-3.2", label: "DeepSeek 3.2" },
  { value: "minimax-m2.5", label: "MiniMax M2.5" },
  { value: "minimax-m2.1", label: "MiniMax M2.1" },
  { value: "glm-5", label: "GLM 5" },
  { value: "qwen3-coder-next", label: "Qwen3 Coder Next" },
];

const MODEL_OPTIONS_BY_ADAPTER: Record<RelayExecutorAdapter, RelayExecutorModelOption[]> = {
  opencode_go: [
    { value: "deepseek-v4-flash", label: "DeepSeek V4 Flash" },
    { value: "deepseek-v4-pro", label: "DeepSeek V4 Pro" },
    { value: "glm-5.2", label: "GLM-5.2" },
    { value: "kimi-k2.6", label: "Kimi K2.6" },
    { value: "kimi-k2.7-code", label: "Kimi K2.7 Code" },
    { value: "mimo-v2.5", label: "MiMo V2.5" },
    { value: "mimo-v2.5-pro", label: "MiMo V2.5 Pro" },
    { value: "minimax-m2.7", label: "MiniMax M2.7" },
    { value: "minimax-m3", label: "MiniMax M3" },
    { value: "qwen3.6-plus", label: "Qwen3.6 Plus" },
    { value: "qwen-3.7-max", label: "Qwen 3.7 Max" },
    { value: "qwen-3.7-plus", label: "Qwen 3.7 Plus" },
  ],
  codex: [
    { value: "gpt-5.4-mini", label: "GPT 5.4 Mini" },
    { value: "gpt-5.4", label: "GPT 5.4" },
    { value: "gpt-5.5", label: "GPT 5.5" },
  ],
  antigravity: [
    { value: "gemini-3.5-flash-low", label: "Gemini 3.5 Flash (Low)" },
    { value: "gemini-3.5-flash-medium", label: "Gemini 3.5 Flash (Medium)" },
    { value: "gemini-3.5-flash-high", label: "Gemini 3.5 Flash (High)" },
    { value: "gemini-3.1-pro-low", label: "Gemini 3.1 Pro (Low)" },
    { value: "gemini-3.1-pro-high", label: "Gemini 3.1 Pro (High)" },
    { value: "claude-sonnet-4.6-thinking", label: "Claude Sonnet 4.6 (Thinking)" },
    { value: "claude-opus-4.6-thinking", label: "Claude Opus 4.6 (Thinking)" },
    { value: "gpt-oss-120b", label: "GPT-OSS 120B" },
  ],
  kiro_cli: KIRO_MODEL_OPTIONS,
};

export function isKnownExecutorAdapter(value?: string): value is RelayExecutorAdapter {
  return EXECUTOR_ADAPTER_OPTIONS.some((option) => option.value === value);
}

export function getModelOptionsForAdapter(
  adapter: string,
  currentModel?: string,
): RelayExecutorModelOption[] {
  const key = isKnownExecutorAdapter(adapter) ? adapter : "opencode_go";
  const options = [...MODEL_OPTIONS_BY_ADAPTER[key]];
  if (currentModel && !options.some((option) => option.value === currentModel)) {
    options.push({ value: currentModel, label: `${currentModel} (current)` });
  }
  return options;
}

export function getDefaultModelForAdapter(adapter: string): string {
  return getModelOptionsForAdapter(adapter)[0]?.value ?? "";
}
