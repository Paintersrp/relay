# Latest Relay Validation Report

- status: passed
- base_commit: ab8544196e05fedf7f56c2d8de4277845e7833ca
- validated_source_snapshot: 0949968cd7b3b3221227a7491f241233efbd7ebde3e2bf403b575c45e0ea0e1b
- worktree_dirty: true
- created_at: 2026-06-21T10:40:05Z

## Validated source changes

- M apps/web/src/components/relay/ApprovalCard.tsx
- M apps/web/src/components/relay/LogPreviewPanel.tsx
- M apps/web/src/components/relay/RelayRunsRegistry.tsx
- ?? apps/web/src/components/relay/RelayRunsRegistryRows.tsx
- M apps/web/src/components/relay/RelayStateSurface.tsx
- M apps/web/src/components/relay/RunEvidenceBrowser.tsx
- M apps/web/src/components/relay/ValidationPanel.tsx
- M apps/web/src/features/relay-runs/mock-data.ts
- M apps/web/src/routes/runs/$runId/intake.tsx
- M apps/web/src/routes/runs/new.tsx
- M apps/web/src/styles.css
- M scripts/validate.sh

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go-fmt-executor` | 0 | passed |
| 2 | `go-test-executor` | 0 | passed |
| 3 | `go-test-all` | 0 | passed |
| 4 | `web-typecheck` | 0 | passed |
| 5 | `web-build` | 0 | passed |

## Failure output tails

No command failures captured.

