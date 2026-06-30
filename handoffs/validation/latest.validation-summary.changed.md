# Latest Relay Validation Report (changed)

- status: passed
- validation_tier: affected
- validation_scope: changed
- base_commit: 8d2cf10fed2028dd24bb3821aecbae73e5f54d06
- validated_source_snapshot: eb459ea18423bf383842ff6c3bbeee0a48ac24bf5a60e7870fe7f4a114449ac2
- worktree_dirty: true
- created_at: 2026-06-30T01:58:22Z

## Affected paths

- docs/operator-guide.md
- Makefile
- scripts/validate.sh

Global escalation required: false

## Validated source changes

- M docs/operator-guide.md
- M Makefile
- M scripts/validate.sh

## Commands

| Step | Name | Command | Exit | Status |
|---:|---|---|---:|---|
| 1 | `validate-script-syntax` | `bash -n scripts/validate.sh` | 0 | passed |

## Failure output tails

No command failures captured.

