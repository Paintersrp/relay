# Guided Feature + Prototype Execution/Cleanup/QA — inspection index

Inspection of the current guided Feature implementation, prototype execution/cleanup-QA,
closure/closing owners, API, web UI, and tests. Read-only; no edits.

## Headline

- Guided journey (`DecideGuidedFeatureAction`) owns the operator-facing continuation for discovery
  -> planning -> delivery ticket -> completion. It has NO prototype execution, cleanup, or QA action
  in its action vocabulary; `guidedPrototype` is display-only read state.
- Prototype execution + cleanup/QA are fully implemented as direct app-service + API endpoints
  (launch/reconcile/cancel/timeout/cleanup/another-execution/qa-packets/evidence), with store,
  executor, app, and API tests. Not reachable from the guided journey or the web UI controls.
- Completion gate matrix has 9 gates; none involve prototype runs, cleanup obligations, or QA
  admission.
- UI "Prototype and QA" section is read-only; no fetch calls to prototype API endpoints exist in
  apps/web.
- MCP role apps expose Wayfinder discovery tools only; no guided or prototype tools.

## Facts (all verified in source)

- App guided owner: `internal/app/features/guided_journey.go` (DecideGuidedFeatureAction, 12 action
  constants), `guided_projection.go` (ReadGuidedProjection, ExecuteGuidedAction, guidedHandoff,
  guidedApprove/PromoteCurrentCandidate), `discovery_lifecycle.go` (CloseFeatureDiscovery,
  AdoptFeatureDiscoveryLifecycle, AssessDiscoveryDestination, RecordDiscoveryDestinationAssessment),
  `service.go` (EvaluateCompletion/Complete + featureCompletionGates).
- Operations projection owners: `internal/app/operations/feature_completion.go`
  (FeatureCompletionWorkflowService), `feature_authority_workflow.go`
  (FeatureAuthorityWorkflowService).
- Prototype app owners: `internal/app/features/prototype_execution.go` (PreparePrototypeProposal,
  PreparePrototypeExecution, ApprovePrototypeExecution, ReadPrototypeExecution),
  `prototype_execution_runtime.go` (Launch/Cancel/Settle/Reconcile via prototypeexecution pkg),
  `prototype_cleanup_qa.go` (ReconcilePrototypeCleanup, PrepareAnotherPrototypeExecution,
  PrepareQADiscoveryPacket, AdmitOperatorQAEvidence, ReadPrototypeEvidenceForWayfinder).
- Prototype store owners: `internal/store/workflow/prototype_execution.go` (Tx proposal/authorization/
  run/approval), `prototype_execution_runtime.go` (runtime/target/lease/evidence transitions),
  `prototype_cleanup_qa.go` (cleanup reconciliations/obligations, QA packet/evidence/admission,
  ClosePrototypeRun), migrations 00038/00039/00040.
- Closing owners: discovery closure packet = store `discovery_lifecycle.go` +
  `SetCurrentDiscoveryClosurePacket`; prototype run close = store `ClosePrototypeRun`; source
  closures = store `source_vaults.go` + `source_vault_publication_reads.go` +
  `GetSourceVaultClosureByRowID/ClosureID`, states importing|ready|unavailable|releasing|released
  (types.go:473-478); completion owner = `service.go` Complete (requires closure packet + manifest
  verify + gates, ErrFeatureCompletionNotReady/Recorded/Confirmation).
- API: `internal/api/features/workspace.go` — MountWorkspaceRoutes: guided GET/POST + guided/actions,
  prototype-runs launch/reconcile/cancel/timeout/cleanup/another-execution, qa-packets, evidence,
  wayfinder-evidence. GuidedProjectionService/GuidedActionService optional interfaces; legacy
  GuidedService path retained for compatibility.
- Server wiring: `internal/server/workflow_routes.go` binds featureAuthorityService
  (SetPrototypeExecutor, SetPrototypeCleaner) and NewWorkspaceHandlerFromServices.
- Web UI: `apps/web/src/routes/feature-workspaces/$workspaceId.tsx` (guided page),
  `components/relay/RelayFeatureWorkspaceDetail.tsx` (read-only "Prototype and QA" section),
  `features/relay-feature-workspaces/api.ts` + `queries.ts` (guided fetch/action only).
- MCP: no prototype/guided/feature-workspace-guided tool references in `internal/mcp`.

## Gap analysis vs likely request (guided journey driving prototype + cleanup/QA)

- G1: No guided action for prepare/approve prototype execution, another-execution, cleanup
  reconcile, QA packet preparation, or QA evidence admission. `GuidedFeatureAction` enum is closed.
- G2: `guidedPrototype` (guided_projection.go:323-348) only surfaces runState/cleanupState/QAState/
  EvidenceState; no decision input or handoff wiring for prototype actions.
- G3: Completion gates (service.go:589-599) exclude prototype/cleanup/QA state; a workspace can be
  "completed" while a prototype run is cleanup_required or QA admission is pending.
- G4: UI has no controls for prototype/QA endpoints; the "Prototype and QA" section is status-only.
- G5: guidedHandoff (guided_projection.go:417-506) covers planning/review/delivery/route only.
- G6: API prototype endpoints require runID/packetID path params and MutationIdentity +
  operatorConfirmationEvidence; guided boundary currently rejects client-supplied identities, so a
  guided action must resolve run/packet server-side like guidedCurrentPlanningCandidate does.

## Normal-entry test locations

- Guided journey: `internal/app/features/guided_journey_test.go`, `guided_projection_test.go`.
- Prototype app: `internal/app/features/prototype_execution_test.go`,
  `prototype_cleanup_qa_test.go`.
- Prototype executor: `internal/executor/prototype_execution_test.go`, `prototype_cleanup_test.go`.
- Prototype store: `internal/store/workflow/feature_workspaces_test.go` (+ feature_workspace_
  investigations_test.go).
- API handlers: `internal/api/features/workspace_test.go` (guided + prototype endpoint tests,
  fakeGuided/richFakeGuided/fakeAuthority).
- Operations projection: `internal/app/operations/feature_completion_test.go`,
  feature_authority_workflow_test.go (in same package dir).
- Web UI: `apps/web/src/components/relay/RelayFeatureWorkspaceDetail.test.tsx`,
  `apps/web/src/features/relay-feature-workspaces/api.test.ts`,
  `apps/web/src/routes/feature-workspaces/scrolling.test.tsx`.

## Dependencies

- app/features -> workflowstore (store) + prototypeexecution (executor primitives) +
  workflowartifacts (artifact staging); guided_projection additionally -> app/tickets +
  app/workflow.
- app/operations -> featureapp (featureapp.RecordAuthorityApprovalInput etc.) + operations/registry.
- API features -> featureapp + appoperations + prototypeexecution + app/workflow + app/tickets.
- server/workflow_routes.go composes featureAuthorityService with prototype executor + cleaner,
  completion workflow service, wayfinder, tickets, packages.
- Store prototype tables gated by workspace.DiscoveryCapabilityEnabled (migration 00011
  feature_workspaces.sql flag).
