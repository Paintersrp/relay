package speccompiler

import (
	"reflect"
	"testing"
)

func TestProjectExecutionSpecPreservesV1ImmutableBaseSemantics(t *testing.T) {
	result, document := CompileExecutionSpec("compiler-fixture.execution-spec.json", readFixture(t, "valid.execution-spec.json"))
	assertSuccess(t, result)
	projection, diagnostics := ProjectExecutionSpec(document)
	if len(diagnostics) != 0 {
		t.Fatalf("projection diagnostics = %+v", diagnostics)
	}
	if projection.SchemaVersion != "1.0" || projection.Replay != ReplayImmutableBase {
		t.Fatalf("projection identity = %+v", projection)
	}
	if len(projection.FileWork) != 4 || len(projection.PathChains) != 4 {
		t.Fatalf("file work or chains = %+v", projection)
	}
	wantRefs := []string{"1.1.file.1", "1.1.file.2", "1.1.file.3", "1.1.file.4"}
	for index, want := range wantRefs {
		if projection.FileWork[index].Ref != want {
			t.Fatalf("file work %d ref = %q want %q", index, projection.FileWork[index].Ref, want)
		}
	}
	modify := projection.FileWork[0]
	if modify.PathChainRef != "chain.1.1.file.1" || len(modify.Directives) != 3 {
		t.Fatalf("modify projection = %+v", modify)
	}
	for _, directive := range modify.Directives {
		if directive.Grounding == nil || !directive.Grounding.BaseRequired || directive.Grounding.ProducerDirectiveRef != "" || directive.Grounding.Replay != ReplayImmutableBase {
			t.Fatalf("v1 grounding = %+v", directive.Grounding)
		}
	}
	renameChain := projection.PathChains[3]
	if !reflect.DeepEqual(renameChain.PathEndpoints, []string{"internal/example/name.go", "internal/example/new_name.go"}) {
		t.Fatalf("rename endpoints = %+v", renameChain.PathEndpoints)
	}
	if !reflect.DeepEqual(projection.ValidationCommands, []ProjectedValidationCommand{ProjectedValidationCommand{Command: "go test ./internal/speccompiler", Expected: "The focused compiler tests pass."}}) {
		t.Fatalf("validation projection = %+v", projection.ValidationCommands)
	}
}

func TestProjectExecutionSpecBuildsV2DependenciesAndGrounding(t *testing.T) {
	result, document := CompileExecutionSpec("compiler-v2-fixture.execution-spec.json", readFixture(t, "valid-v2.execution-spec.json"))
	assertSuccess(t, result)
	projection, diagnostics := ProjectExecutionSpec(document)
	if len(diagnostics) != 0 {
		t.Fatalf("projection diagnostics = %+v", diagnostics)
	}
	if projection.Replay != ReplayEvolvingPathChain || len(projection.PathChains) != 1 || len(projection.FileWork) != 4 {
		t.Fatalf("v2 projection = %+v", projection)
	}
	if projection.PathChains[0].Ref != "chain.1.1.file.1" || !reflect.DeepEqual(projection.PathChains[0].FileWorkRefs, []string{"1.1.file.1", "1.2.file.1", "1.3.file.1", "1.4.file.1"}) {
		t.Fatalf("path chain = %+v", projection.PathChains[0])
	}
	first, second, third, fourth := projection.Substeps[0], projection.Substeps[1], projection.Substeps[2], projection.Substeps[3]
	if first.AtomicPresent || second.AtomicPresent || !third.AtomicPresent || third.Atomic || !fourth.AtomicPresent || !fourth.Atomic {
		t.Fatalf("atomic metadata = first=%+v second=%+v third=%+v fourth=%+v", first, second, third, fourth)
	}
	if !reflect.DeepEqual(second.AuthoredDependencies, []string{"1.1"}) || !reflect.DeepEqual(second.ImplicitDependencies, []string{"1.1"}) || !reflect.DeepEqual(second.Dependencies, []string{"1.1"}) {
		t.Fatalf("dependency-only projection = %+v", second)
	}
	if len(third.AuthoredDependencies) != 0 || !reflect.DeepEqual(third.ImplicitDependencies, []string{"1.2"}) || !reflect.DeepEqual(third.Dependencies, []string{"1.2"}) {
		t.Fatalf("atomic-only projection = %+v", third)
	}
	if !reflect.DeepEqual(fourth.AuthoredDependencies, []string{"1.3"}) || !reflect.DeepEqual(fourth.ImplicitDependencies, []string{"1.3"}) || !reflect.DeepEqual(fourth.Dependencies, []string{"1.3"}) {
		t.Fatalf("combined projection = %+v", fourth)
	}
	for _, work := range projection.FileWork {
		if !reflect.DeepEqual(work.SourcePreconditions, []ProjectedSourcePrecondition{ProjectedSourcePrecondition{Kind: "path_exists", Path: "internal/example/config.go"}}) {
			t.Fatalf("source preconditions for %s = %+v", work.Ref, work.SourcePreconditions)
		}
	}
	wantGrounding := []struct {
		baseRequired bool
		producer     string
	}{
		{baseRequired: true},
		{producer: "1.1.file.1.change.1"},
		{producer: "1.2.file.1.change.1"},
		{producer: "1.3.file.1.change.1"},
	}
	for index, want := range wantGrounding {
		grounding := projection.FileWork[index].Directives[0].Grounding
		if grounding == nil || grounding.BaseRequired != want.baseRequired || grounding.ProducerDirectiveRef != want.producer {
			t.Fatalf("grounding %d = %+v want base=%t producer=%q", index, grounding, want.baseRequired, want.producer)
		}
	}
}

func TestProjectExecutionSpecDoesNotUseLaterOrCrossChainProducers(t *testing.T) {
	result, document := CompileExecutionSpec("grounding-boundary.execution-spec.json", executionV2GroundingBoundaryDocument())
	assertSuccess(t, result)
	projection, diagnostics := ProjectExecutionSpec(document)
	if len(diagnostics) != 0 {
		t.Fatalf("projection diagnostics = %+v", diagnostics)
	}
	byRef := map[string]ProjectedFileWork{}
	for _, work := range projection.FileWork {
		byRef[work.Ref] = work
	}
	for _, ref := range []string{"1.1.file.1", "1.2.file.1"} {
		grounding := byRef[ref].Directives[0].Grounding
		if grounding == nil || !grounding.BaseRequired || grounding.ProducerDirectiveRef != "" {
			t.Fatalf("%s grounding = %+v", ref, grounding)
		}
	}
}

func executionV2GroundingBoundaryDocument() []byte {
	return []byte(`{
  "schema_version": "2.0",
  "feature_slug": "grounding-boundary",
  "repo_target": "relay",
  "branch": "main",
  "base_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "goal": "Exercise selector producer boundaries.",
  "context": "Grounding boundary fixture.",
  "scope": {
    "in_scope": ["Exercise later and cross-chain producer boundaries."],
    "out_of_scope": ["Do not mutate repositories."]
  },
  "steps": [
    {
      "number": 1,
      "goal": "Keep invalid producer relationships unlinked.",
      "substeps": [
        {
          "number": 1,
          "instruction": "Consume text that is produced only by a later directive.",
          "files": [
            {
              "path": "internal/example/later.go",
              "operation": "modify",
              "purpose": "Exercise the later-producer boundary.",
              "implementation": {
                "changes": [
                  {
                    "kind": "replace",
                    "old_text": "const later = true\n",
                    "new_text": "const consumed = true\n",
                    "expected_occurrences": 1
                  }
                ]
              }
            }
          ],
          "completion_criteria": ["The consumer remains base-required."]
        },
        {
          "number": 2,
          "instruction": "Consume text produced earlier on an unrelated path.",
          "files": [
            {
              "path": "internal/example/consumer.go",
              "operation": "modify",
              "purpose": "Exercise the cross-chain producer boundary.",
              "implementation": {
                "changes": [
                  {
                    "kind": "replace",
                    "old_text": "const cross = true\n",
                    "new_text": "const crossConsumed = true\n",
                    "expected_occurrences": 1
                  }
                ]
              }
            },
            {
              "path": "internal/example/producer.go",
              "operation": "modify",
              "purpose": "Produce matching text on another path.",
              "implementation": {
                "changes": [
                  {
                    "kind": "replace_file",
                    "content": "package example\n\nconst cross = true\n"
                  }
                ]
              }
            },
            {
              "path": "internal/example/later.go",
              "operation": "modify",
              "purpose": "Produce the first consumer selector too late.",
              "implementation": {
                "changes": [
                  {
                    "kind": "replace_file",
                    "content": "package example\n\nconst later = true\n"
                  }
                ]
              }
            }
          ],
          "completion_criteria": ["Unrelated and later producers remain unlinked."]
        }
      ],
      "completion_criteria": ["Only earlier same-chain producers can satisfy grounding."]
    }
  ],
  "validation": {
    "commands": [
      {"command": "go test ./internal/speccompiler", "expected": "The focused compiler tests pass."}
    ]
  },
  "completion_criteria": ["Producer boundaries are deterministic."]
}
`)
}

func TestProjectExecutionSpecConnectsRenameEndpoints(t *testing.T) {
	result, document := CompileExecutionSpec("rename-chain.execution-spec.json", executionV2RenameChainDocument())
	assertSuccess(t, result)
	projection, diagnostics := ProjectExecutionSpec(document)
	if len(diagnostics) != 0 {
		t.Fatalf("projection diagnostics = %+v", diagnostics)
	}
	if len(projection.PathChains) != 1 {
		t.Fatalf("path chains = %+v", projection.PathChains)
	}
	chain := projection.PathChains[0]
	if !reflect.DeepEqual(chain.FileWorkRefs, []string{"1.1.file.1", "1.2.file.1"}) ||
		!reflect.DeepEqual(chain.PathEndpoints, []string{"internal/example/old.go", "internal/example/new.go"}) ||
		!reflect.DeepEqual(chain.SubstepRefs, []string{"1.1", "1.2"}) {
		t.Fatalf("rename chain = %+v", chain)
	}
	if !reflect.DeepEqual(projection.Substeps[1].ImplicitDependencies, []string{"1.1"}) {
		t.Fatalf("rename dependency = %+v", projection.Substeps[1])
	}
	if !reflect.DeepEqual(projection.FileWork[0].SourcePreconditions, []ProjectedSourcePrecondition{
		{Kind: "path_exists", Path: "internal/example/old.go"},
		{Kind: "path_absent", Path: "internal/example/new.go"},
	}) {
		t.Fatalf("rename preconditions = %+v", projection.FileWork[0].SourcePreconditions)
	}
}

func executionV2RenameChainDocument() []byte {
	return []byte(`{
  "schema_version": "2.0",
  "feature_slug": "rename-chain",
  "repo_target": "relay",
  "branch": "main",
  "base_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "goal": "Exercise rename path-chain construction.",
  "context": "Rename chain fixture.",
  "scope": {
    "in_scope": ["Exercise rename endpoints."],
    "out_of_scope": ["Do not mutate repositories."]
  },
  "steps": [
    {
      "number": 1,
      "goal": "Connect rename work.",
      "substeps": [
        {
          "number": 1,
          "instruction": "Rename the source file.",
          "files": [
            {
              "path": "internal/example/old.go",
              "destination_path": "internal/example/new.go",
              "operation": "rename",
              "purpose": "Join source and destination endpoints.",
              "implementation": {"preserve_content": true}
            }
          ],
          "completion_criteria": ["The rename is declared."]
        },
        {
          "number": 2,
          "instruction": "Modify the rename destination.",
          "files": [
            {
              "path": "internal/example/new.go",
              "operation": "modify",
              "purpose": "Join later work to the destination endpoint.",
              "implementation": {
                "changes": [
                  {
                    "kind": "replace",
                    "old_text": "const oldName = true\n",
                    "new_text": "const newName = true\n",
                    "expected_occurrences": 1
                  }
                ]
              }
            }
          ],
          "completion_criteria": ["The destination work is declared."]
        }
      ],
      "completion_criteria": ["The rename path chain is complete."]
    }
  ],
  "validation": {
    "commands": [
      {"command": "go test ./internal/speccompiler", "expected": "The focused compiler tests pass."}
    ]
  },
  "completion_criteria": ["Rename endpoints form one path chain."]
}
`)
}
