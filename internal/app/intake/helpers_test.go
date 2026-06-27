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
		metadata  map[string]string
		wantModel string
	}{
		{
			name:      "no metadata defaults to deepseek-v4-pro",
			metadata:  nil,
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "empty metadata defaults to deepseek-v4-pro",
			metadata:  map[string]string{},
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "explicit recommended_model wins",
			metadata:  map[string]string{"recommended_model": "gpt-5", "model": "claude"},
			wantModel: "gpt-5",
		},
		{
			name:      "executor_model_profile wins when recommended_model missing",
			metadata:  map[string]string{"executor_model_profile": "deepseek-v4-pro", "model": "gemini"},
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "model key wins when others missing",
			metadata:  map[string]string{"model": "deepseek-v4-flash"},
			wantModel: "deepseek-v4-flash",
		},
		{
			name:      "target_executor deepseek maps to deepseek-v4-pro",
			metadata:  map[string]string{"target_executor": "deepseek"},
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "target_executor deepseek-v4-pro maps to deepseek-v4-pro",
			metadata:  map[string]string{"target_executor": "deepseek-v4-pro"},
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "target_executor deepseek-v4-flash maps to deepseek-v4-flash",
			metadata:  map[string]string{"target_executor": "deepseek-v4-flash"},
			wantModel: "deepseek-v4-flash",
		},
		{
			name:      "target_executor opencode defaults to deepseek-v4-pro",
			metadata:  map[string]string{"target_executor": "opencode"},
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "target_executor opencode_go defaults to deepseek-v4-pro",
			metadata:  map[string]string{"target_executor": "opencode_go"},
			wantModel: "deepseek-v4-pro",
		},
		{
			name:      "target_executor unknown still defaults to deepseek-v4-pro",
			metadata:  map[string]string{"target_executor": "cline"},
			wantModel: "deepseek-v4-pro",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := resolveIntakeRecommendedModel(tc.metadata)
			if model != tc.wantModel {
				t.Errorf("expected model %q, got %q", tc.wantModel, model)
			}
		})
	}
}
