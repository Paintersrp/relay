# Latest Relay Validation Report

- status: failed
- base_commit: a9950a3b3142e9b3fef23714fa2d236ac311655a
- validated_source_snapshot: f276182aa683ca570553850e9e7a8e56264df8e983c6de03749e3b924bf58062
- worktree_dirty: true
- created_at: 2026-06-21T16:01:29Z

## Validated source changes

- M apps/web/src/components/relay/RunIntakeReviewPanel.tsx

## Commands

| Step | Name | Exit | Status |
|---:|---|---:|---|
| 1 | `go-fmt-executor` | 0 | passed |
| 2 | `go-test-executor` | 0 | passed |
| 3 | `go-test-all` | 0 | passed |
| 4 | `web-typecheck` | 2 | failed |
| 5 | `web-build` | 0 | passed |

## Failure output tails

### web-typecheck

```text
$ cd apps/web && npm run typecheck

> relay-web@0.1.0 typecheck
> tsc --noEmit

src/components/relay/RunIntakeReviewPanel.tsx(97,7): error TS2322: Type 'string' is not assignable to type '"codex" | "antigravity" | "deepseek-v4-flash" | "deepseek/deepseek-chat-v3-0324:free" | "anthropic/claude-sonnet-4-5" | "openrouter/auto"'.
src/components/relay/RunIntakeReviewPanel.tsx(98,7): error TS2322: Type '`${string} (current)`' is not assignable to type '"OpenRouter Auto" | "DeepSeek V4 Flash" | "DeepSeek Chat V3 0324" | "Claude Sonnet 4.5" | "Codex Default" | "Antigravity Default"'.
exit_code: 2

```

