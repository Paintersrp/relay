package executor

import (
	"reflect"
	"testing"
	"time"

	"relay/internal/pipeline"
)

func TestAdaptersUseAttemptLocalModelAndExactBrief(t *testing.T) {
	brief := "# Executor Brief\n\nExact bytes.\n"
	request := ExecutorAdapterRequest{
		RunID:         1,
		RepoPath:      t.TempDir(),
		BriefContent:  brief,
		BriefPath:     "executor-brief.md",
		ResultPath:    "codex-result.txt",
		SelectedModel: "attempt-model",
		Timeout:       time.Minute,
	}

	codex := &CodexAdapter{Config: CodexAdapterConfig{Binary: "codex", Model: "configured-model", Sandbox: "workspace-write"}}
	codexInvocation, err := codex.BuildInvocation(request)
	if err != nil {
		t.Fatal(err)
	}
	if codexInvocation.Model != "attempt-model" || codexInvocation.Stdin != brief || !reflect.DeepEqual(codexInvocation.ResultFile, "codex-result.txt") {
		t.Fatalf("Codex invocation = %+v", codexInvocation)
	}

	antigravity := &AntigravityAdapter{Config: AntigravityAdapterConfig{Binary: "antigravity", Model: "configured-model", ApproveFlag: "--yes"}}
	antigravityInvocation, err := antigravity.BuildInvocation(request)
	if err != nil {
		t.Fatal(err)
	}
	if antigravityInvocation.Model != "attempt-model" || antigravityInvocation.Stdin != "" {
		t.Fatalf("Antigravity invocation = %+v", antigravityInvocation)
	}
	foundBriefPath := false
	for _, arg := range antigravityInvocation.Args {
		if arg == request.BriefPath {
			foundBriefPath = true
		}
	}
	if !foundBriefPath {
		t.Fatalf("Antigravity args do not contain exact brief path: %v", antigravityInvocation.Args)
	}

	kiro := &KiroCLIAdapter{Config: KiroCLIAdapterConfig{Binary: "kiro-cli", Model: "configured-model", Effort: "high", TrustTools: defaultKiroTrustTools}}
	kiroRequest := request
	kiroRequest.SelectedModel = "auto"
	kiroInvocation, err := kiro.BuildInvocation(kiroRequest)
	if err != nil {
		t.Fatal(err)
	}
	if kiroInvocation.Model != "auto" || kiroInvocation.Stdin != brief {
		t.Fatalf("Kiro invocation = %+v", kiroInvocation)
	}

	openCode := &OpenCodeAdapter{Config: pipeline.OpenCodeRunConfig{Binary: "opencode", Agent: "build"}}
	openCodeRequest := request
	openCodeRequest.SelectedModel = "openai/gpt-5.5"
	openCodeInvocation, err := openCode.BuildInvocation(openCodeRequest)
	if err == nil && openCodeInvocation.Stdin != brief {
		t.Fatalf("OpenCode changed brief bytes: %+v", openCodeInvocation)
	}
}
