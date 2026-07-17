package workflowstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestMCPMutationResultPersistenceImmutabilityAndExactKey(t *testing.T) {
	ctx := context.Background()
	store, _ := openWorkflowTestStore(t)
	key := MCPMutationKey{SurfaceContractID: "planner-plan.v1", ToolName: "submit_plan", MutationID: "mutation-1"}
	resultJSON := submitPlanResultJSON("plan-1")
	resultSHA := mutationResultSHA(resultJSON)
	var created MCPMutationResult
	if err := store.WithTx(ctx, func(tx *Tx) error {
		var err error
		created, err = tx.CreateMCPMutationResult(ctx, CreateMCPMutationResultParams{
			SurfaceContractID:       key.SurfaceContractID,
			ToolName:                key.ToolName,
			MutationID:              key.MutationID,
			SurfaceManifestSHA256:   strings.Repeat("a", 64),
			SemanticIdentityVersion: "relay.semantic.submit-plan.v1",
			SemanticRequestSHA256:   strings.Repeat("b", 64),
			ResultKind:              "submit_plan_result",
			ResultIdentityJSON:      resultJSON,
			ResultSHA256:            resultSHA,
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.GetMCPMutationResultOptional(ctx, key)
	if err != nil || !ok || got.ID != created.ID || got.ResultIdentityJSON != resultJSON || got.ResultSHA256 != resultSHA {
		t.Fatalf("lookup = %#v, %v, %v", got, ok, err)
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		txGot, txOK, err := tx.GetMCPMutationResultOptional(ctx, key)
		if err != nil || !txOK || txGot.ID != created.ID {
			t.Fatalf("transaction lookup = %#v, %v, %v", txGot, txOK, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`UPDATE mcp_mutation_results SET result_kind = 'other' WHERE id = ?`, created.ID); err == nil {
		t.Fatal("mutation result update succeeded")
	}
	if _, err := store.DB().Exec(`DELETE FROM mcp_mutation_results WHERE id = ?`, created.ID); err == nil {
		t.Fatal("mutation result delete succeeded")
	}
	if err := store.WithTx(ctx, func(tx *Tx) error {
		_, err := tx.CreateMCPMutationResult(ctx, CreateMCPMutationResultParams{
			SurfaceContractID:       key.SurfaceContractID,
			ToolName:                key.ToolName,
			MutationID:              key.MutationID,
			SurfaceManifestSHA256:   strings.Repeat("a", 64),
			SemanticIdentityVersion: "other",
			SemanticRequestSHA256:   strings.Repeat("c", 64),
			ResultKind:              "submit_plan_result",
			ResultIdentityJSON:      submitPlanResultJSON("plan-2"),
			ResultSHA256:            mutationResultSHA(submitPlanResultJSON("plan-2")),
		})
		return err
	}); err == nil {
		t.Fatal("duplicate mutation key succeeded")
	}
}

func TestMCPMutationResultDatabaseClosesSurfaceToolKindAndSize(t *testing.T) {
	store, _ := openWorkflowTestStore(t)
	validJSON := submitPlanResultJSON("plan-1")
	validArgs := []any{
		"planner-plan.v1",
		"submit_plan",
		"mutation-1",
		strings.Repeat("a", 64),
		"relay.semantic.submit-plan.v1",
		strings.Repeat("b", 64),
		"submit_plan_result",
		validJSON,
		mutationResultSHA(validJSON),
	}
	insert := `INSERT INTO mcp_mutation_results (
		surface_contract_id, tool_name, mutation_id, surface_manifest_sha256,
		semantic_identity_version, semantic_request_sha256, result_kind,
		result_identity_json, result_sha256
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	cases := []struct {
		name string
		edit func([]any)
	}{
		{name: "invalid surface", edit: func(v []any) { v[0] = "unknown.v1" }},
		{name: "cross surface tool", edit: func(v []any) { v[0] = "planner-execution.v1" }},
		{name: "unknown tool", edit: func(v []any) { v[1] = "other_tool" }},
		{name: "invalid mutation id", edit: func(v []any) { v[2] = "bad space" }},
		{name: "wrong result kind", edit: func(v []any) { v[6] = "create_run_result" }},
		{name: "non compact result", edit: func(v []any) { v[7] = `{ "plan_id":"plan-1" }`; v[8] = mutationResultSHA(v[7].(string)) }},
		{name: "oversized result", edit: func(v []any) {
			v[7] = `{"value":"` + strings.Repeat("x", 65536) + `"}`
			v[8] = mutationResultSHA(v[7].(string))
		}},
		{name: "oversized multibyte result", edit: func(v []any) {
			v[7] = `{"value":"` + strings.Repeat("Ã©", 32768) + `"}`
			v[8] = mutationResultSHA(v[7].(string))
		}},
	}
	for index, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			args := append([]any(nil), validArgs...)
			args[2] = "mutation-" + string(rune('a'+index))
			test.edit(args)
			if _, err := store.DB().Exec(insert, args...); err == nil {
				t.Fatal("invalid mutation result row was accepted")
			}
		})
	}
}

func TestMCPMutationResultTableIsTheOnlyNewDurableClass(t *testing.T) {
	store, _ := openWorkflowTestStore(t)
	for _, table := range []string{
		"mcp_input_receipts",
		"mcp_clearance_gates",
		"mcp_pending_mutations",
		"mcp_failed_mutations",
		"mcp_mutation_attempts",
		"mcp_mutation_expirations",
	} {
		var count int
		if err := store.DB().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("unexpected table %q", table)
		}
	}
}

func submitPlanResultJSON(planID string) string {
	return `{"plan_id":"` + planID + `","artifact_id":"artifact-1","artifact_sha256":"` + strings.Repeat("a", 64) + `","project_id":"project-1","submission_id":"submission-1","workflow_state":"active","complete":true}`
}

func mutationResultSHA(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
