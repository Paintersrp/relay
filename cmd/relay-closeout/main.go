package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"relay/internal/closeout"
)

func main() {
	var (
		messageFlag            string
		slugFlag               string
		dryRun                 bool
		promoteRuntimeEvidence bool
		projectID              string
		repoTarget             string
		runID                  string
		planID                 string
		passID                 string
		baseRef                string
		headRef                string
	)

	flag.StringVar(&messageFlag, "message", "", "commit message")
	flag.StringVar(&slugFlag, "slug", "", "closeout artifact slug")
	flag.BoolVar(&dryRun, "dry-run", false, "write evidence and report would-stage paths without staging, committing, or pushing")
	flag.BoolVar(&promoteRuntimeEvidence, "promote-runtime-evidence", false, "include Relay-managed runtime evidence in explicit staging (env: RELAY_CLOSEOUT_PROMOTE_RUNTIME_EVIDENCE=1)")
	flag.StringVar(&projectID, "project-id", "", "closeout project_id (default: relay, env: RELAY_CLOSEOUT_PROJECT_ID)")
	flag.StringVar(&repoTarget, "repo-target", "", "closeout repo_target (default: Paintersrp/relay, env: RELAY_CLOSEOUT_REPO_TARGET)")
	flag.StringVar(&runID, "run-id", "", "closeout run_id (default: local-closeout, env: RELAY_CLOSEOUT_RUN_ID)")
	flag.StringVar(&planID, "plan-id", "", "closeout plan_id (env: RELAY_CLOSEOUT_PLAN_ID)")
	flag.StringVar(&passID, "pass-id", "", "closeout pass_id (env: RELAY_CLOSEOUT_PASS_ID)")
	flag.StringVar(&baseRef, "base-ref", "", "branch_context.base_ref (nullable when empty)")
	flag.StringVar(&headRef, "head-ref", "", "branch_context.head_ref (defaults to current branch when empty)")
	flag.Parse()

	message := messageFlag
	if message == "" {
		message = os.Getenv("MESSAGE")
	}
	slug := slugFlag
	if slug == "" {
		slug = os.Getenv("SLUG")
	}
	if message == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --message or MESSAGE is required")
		os.Exit(2)
	}

	report, err := closeout.Run(context.Background(), closeout.Options{
		Message:                message,
		Slug:                   slug,
		DryRun:                 dryRun,
		ProjectID:              projectID,
		RepoTarget:             repoTarget,
		RunID:                  runID,
		PlanID:                 planID,
		PassID:                 passID,
		BaseRef:                baseRef,
		HeadRef:                headRef,
		PromoteRuntimeEvidence: promoteRuntimeEvidence,
	})

	fmt.Printf("validation: %s\n", report.ValidationStatus())
	fmt.Printf("evidence_json: %s\n", filepath.ToSlash(report.EvidenceJSONPath()))
	fmt.Printf("evidence_markdown: %s\n", filepath.ToSlash(report.EvidenceMarkdownPath()))
	fmt.Printf("commit: %s\n", report.CommitStatus())
	fmt.Printf("push: %s\n", report.PushStatus())

	if err != nil {
		if blocker, ok := err.(*closeout.MechanicalBlockerError); ok {
			fmt.Printf("mechanical_blocker: %s\n", blocker.Stage)
		}
		os.Exit(1)
	}
}
