package intake

import "testing"

func TestResolveIntakeExecutorAdapter(t *testing.T) {
	cases := []struct {
		name         string
		input        IntakeInput
		metadata     map[string]string
		wantAdapter  string
		wantExplicit bool
		wantErr      bool
	}{
		{
			name:         "explicit codex in input",
			input:        IntakeInput{ExecutorAdapter: "codex"},
			metadata:     nil,
			wantAdapter:  "codex",
			wantExplicit: true,
			wantErr:      false,
		},
		{
			name:         "snake_case agy alias in metadata",
			input:        IntakeInput{},
			metadata:     map[string]string{"executor_adapter": "agy"},
			wantAdapter:  "antigravity",
			wantExplicit: true,
			wantErr:      false,
		},
		{
			name:         "invalid metadata executor_adapter",
			input:        IntakeInput{},
			metadata:     map[string]string{"executor_adapter": "invalid_adapter"},
			wantAdapter:  "",
			wantExplicit: true,
			wantErr:      true,
		},
		{
			name:         "target_executor codex maps as explicit adapter fallback",
			input:        IntakeInput{},
			metadata:     map[string]string{"target_executor": "codex"},
			wantAdapter:  "codex",
			wantExplicit: true,
			wantErr:      false,
		},
		{
			name:         "target_executor agy maps as explicit antigravity adapter fallback",
			input:        IntakeInput{},
			metadata:     map[string]string{"target_executor": "agy"},
			wantAdapter:  "antigravity",
			wantExplicit: true,
			wantErr:      false,
		},
		{
			name:         "target_executor deepseek-v4-flash defaulting without error",
			input:        IntakeInput{},
			metadata:     map[string]string{"target_executor": "deepseek-v4-flash"},
			wantAdapter:  "opencode_go",
			wantExplicit: false,
			wantErr:      false,
		},
		{
			name:         "no fields defaulting without error",
			input:        IntakeInput{},
			metadata:     nil,
			wantAdapter:  "opencode_go",
			wantExplicit: false,
			wantErr:      false,
		},
		{
			name:         "codex_cli alias normalizes to codex",
			input:        IntakeInput{ExecutorAdapter: "codex_cli"},
			metadata:     nil,
			wantAdapter:  "codex",
			wantExplicit: true,
			wantErr:      false,
		},
		{
			name:         "antigravity_cli alias normalizes to antigravity",
			input:        IntakeInput{ExecutorAdapter: "antigravity_cli"},
			metadata:     nil,
			wantAdapter:  "antigravity",
			wantExplicit: true,
			wantErr:      false,
		},
		{
			name:         "target_executor antigravity maps via fallback",
			input:        IntakeInput{},
			metadata:     map[string]string{"target_executor": "antigravity"},
			wantAdapter:  "antigravity",
			wantExplicit: true,
			wantErr:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, explicit, err := resolveIntakeExecutorAdapter(tc.input, tc.metadata)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if adapter != tc.wantAdapter {
				t.Errorf("expected adapter %q, got %q", tc.wantAdapter, adapter)
			}
			if explicit != tc.wantExplicit {
				t.Errorf("expected explicit=%v, got %v", tc.wantExplicit, explicit)
			}
		})
	}
}

func TestResolveIntakeRecommendedModel(t *testing.T) {
	cases := []struct {
		name      string
		input     IntakeInput
		metadata  map[string]string
		adapter   string
		wantModel string
	}{
		{
			name:      "no metadata defaults to deepseek-v4-pro",
			input:     IntakeInput{},
			metadata:  nil,
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "empty metadata defaults to deepseek-v4-pro",
			input:     IntakeInput{},
			metadata:  map[string]string{},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "explicit recommended_model wins",
			input:     IntakeInput{},
			metadata:  map[string]string{"recommended_model": "gpt-5", "model": "claude"},
			adapter:   "opencode_go",
			wantModel: "gpt-5",
		},
		{
			name:      "executor_model_profile wins when recommended_model missing",
			input:     IntakeInput{},
			metadata:  map[string]string{"executor_model_profile": "deepseek-v4-pro", "model": "gemini"},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "model key wins when others missing",
			input:     IntakeInput{},
			metadata:  map[string]string{"model": "deepseek-v4-flash"},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-flash",
		},
		{
			name:      "target_executor deepseek maps to deepseek-v4-pro",
			input:     IntakeInput{},
			metadata:  map[string]string{"target_executor": "deepseek"},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "target_executor deepseek-v4-pro maps to deepseek-v4-pro",
			input:     IntakeInput{},
			metadata:  map[string]string{"target_executor": "deepseek-v4-pro"},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "target_executor deepseek-v4-flash maps to deepseek-v4-flash",
			input:     IntakeInput{},
			metadata:  map[string]string{"target_executor": "deepseek-v4-flash"},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-flash",
		},
		{
			name:      "target_executor opencode defaults to deepseek-v4-pro",
			input:     IntakeInput{},
			metadata:  map[string]string{"target_executor": "opencode"},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "target_executor opencode_go defaults to deepseek-v4-pro",
			input:     IntakeInput{},
			metadata:  map[string]string{"target_executor": "opencode_go"},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "target_executor unknown still defaults to deepseek-v4-pro",
			input:     IntakeInput{},
			metadata:  map[string]string{"target_executor": "cline"},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "explicit input ExecutorModelProfile overrides metadata",
			input:     IntakeInput{ExecutorModelProfile: "claude-4"},
			metadata:  map[string]string{"recommended_model": "gpt-5"},
			adapter:   "opencode_go",
			wantModel: "claude-4",
		},
		{
			name:      "explicit input ExecutorModelProfile2 overrides metadata",
			input:     IntakeInput{ExecutorModelProfile2: "claude-sonnet-4.6"},
			metadata:  map[string]string{"recommended_model": "deepseek-v4-pro"},
			adapter:   "opencode_go",
			wantModel: "claude-sonnet-4.6",
		},
		{
			name:      "explicit input RecommendedModel overrides metadata",
			input:     IntakeInput{RecommendedModel: "claude-opus-4.6"},
			metadata:  map[string]string{"executor_model_profile": "deepseek-v4-pro"},
			adapter:   "opencode_go",
			wantModel: "claude-opus-4.6",
		},
		{
			name:      "explicit input ExecutorModelProfile overrides target_executor fallback",
			input:     IntakeInput{ExecutorModelProfile: "deepseek-v4-pro"},
			metadata:  map[string]string{"target_executor": "deepseek-v4-flash"},
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "codex metadata preserves explicit model",
			input:     IntakeInput{},
			metadata:  map[string]string{"target_executor": "codex", "recommended_model": "gpt-5"},
			adapter:   "codex",
			wantModel: "gpt-5",
		},
		{
			name:      "antigravity metadata preserves explicit model",
			input:     IntakeInput{},
			metadata:  map[string]string{"target_executor": "antigravity", "executor_model_profile": "deepseek-v4-pro"},
			adapter:   "antigravity",
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "kiro_cli plus no explicit model resolves to auto",
			input:     IntakeInput{},
			metadata:  nil,
			adapter:   "kiro_cli",
			wantModel: "auto",
		},
		{
			name:      "kiro_cli plus executor_model_profile claude-sonnet-4.6 resolves to claude-sonnet-4.6",
			input:     IntakeInput{},
			metadata:  map[string]string{"executor_model_profile": "claude-sonnet-4.6"},
			adapter:   "kiro_cli",
			wantModel: "claude-sonnet-4.6",
		},
		{
			name:      "kiro_cli plus recommended_model qwen3-coder-next resolves to qwen3-coder-next",
			input:     IntakeInput{},
			metadata:  map[string]string{"recommended_model": "qwen3-coder-next"},
			adapter:   "kiro_cli",
			wantModel: "qwen3-coder-next",
		},
		{
			name:      "opencode_go without a model keeps deepseek-v4-pro default",
			input:     IntakeInput{},
			metadata:  nil,
			adapter:   "opencode_go",
			wantModel: "deepseek-v4-pro",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := resolveIntakeRecommendedModel(tc.input, tc.metadata, tc.adapter)
			if model != tc.wantModel {
				t.Errorf("expected model %q, got %q", tc.wantModel, model)
			}
		})
	}
}
