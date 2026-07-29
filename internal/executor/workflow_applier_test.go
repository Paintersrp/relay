package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	workflowruns "relay/internal/app/runs/workflow"
	workflowstore "relay/internal/store/workflow"
)

func createRunWithCanonicalProjectionSpec(t *testing.T, fixture *workflowFixture, slug string) workflowstore.Run {
	t.Helper()
	repository, err := fixture.store.GetRepositoryTarget(context.Background(), "relay")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository.LocalPath, "deterministic.txt"), []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository.LocalPath, "residual.txt"), []byte("source\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	canonical := []byte(`{
  "schema_version": "2.0",
  "feature_slug": "` + slug + `",
  "repo_target": "relay",
  "branch": "feat/simplification",
  "base_commit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
  "goal": "Exercise deterministic-first workflow behavior.",
  "context": "Canonical workflow fixture projected directly by the compiler.",
  "scope": {
    "in_scope": ["Exercise deterministic and residual file work."],
    "out_of_scope": ["No unrelated behavior."]
  },
  "steps": [
    {
      "number": 1,
      "goal": "Provide deterministic and residual declarations.",
      "substeps": [
        {
          "number": 1,
          "instruction": "Apply deterministic work.",
          "files": [
            {
              "path": "deterministic.txt",
              "operation": "modify",
              "purpose": "Apply deterministic work.",
              "implementation": {
                "changes": [
                  {
                    "kind": "replace",
                    "old_text": "before\n",
                    "new_text": "after\n",
                    "expected_occurrences": 1
                  }
                ]
              }
            }
          ],
          "completion_criteria": ["The deterministic declaration is complete."]
        },
        {
          "number": 2,
          "instruction": "Apply residual work.",
          "files": [
            {
              "path": "residual.txt",
              "destination_path": "residual-renamed.txt",
              "operation": "rename",
              "purpose": "Exercise model-owned rename replacement content.",
              "implementation": {
                "content": "replacement\n"
              }
            }
          ],
          "completion_criteria": ["The residual declaration is complete."]
        }
      ],
      "completion_criteria": ["The selected declarations are complete."]
    }
  ],
  "validation": {
    "commands": [
      {
        "command": "go test ./internal/executor",
        "expected": "The focused executor tests pass."
      }
    ]
  },
  "completion_criteria": ["The combined deterministic and residual result is complete."]
}
`)
	created, err := fixture.runs.CreateRun(context.Background(), workflowruns.CreateRunInput{
		FeatureSlug:      slug,
		RepoTarget:       "relay",
		Branch:           "feat/simplification",
		BaseCommit:       strings.Repeat("b", 40),
		CanonicalJSON:    canonical,
		RenderedMarkdown: fixture.brief,
	})
	if err != nil {
		t.Fatal(err)
	}
	return created.Run
}
