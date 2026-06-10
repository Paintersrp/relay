# Relay Run Audit Handoff

## Run

- Run ID: 1
- Title: Test Run
- Repo: test-repo
- Branch: main
- Status: draft

## Original Handoff

```
# Test

## Goal
Do something.

```

## Agent Result

- Status: DONE
- Build status: N/A
- Test status: N/A
- LOC changed: N/A

Raw result excerpt:
```
DONE
```

## Relay Validation

- Status: pass
- Repo path: C:\Users\trist\AppData\Local\Temp\TestValidationWorkerErrorFinalizesProgress2974225074\002
- Commands:
  - `go version` pass exit 0 63ms stdout

## Artifacts

- agent_prompt
- opencode_handoff_packet
- agent_result_raw
- validation_run_json
- validation_stdout
- validation_stderr
- opencode_stdout
- opencode_stderr
- opencode_combined_log
- git_status_text
- git_diff_stat
- git_diff_numstat
- git_diff_name_status
- git_diff_patch

## Git Diff Evidence

### Git status

```text
M README.md
```

### Diff stat

```text
README.md | 4 +++-
 1 file changed, 3 insertions(+), 1 deletion(-)
```

### Changed files

```text
M	README.md
```

### Patch

Patch artifact: git_diff_patch

Small excerpt:
```diff
diff --git a/README.md b/README.md
index f8051e0..70c57ad 100644
--- a/README.md
+++ b/README.md
@@ -1 +1,3 @@
-# Repo
+# Modified
+
+Changed.
```

## Review request

Please audit whether the implementation appears complete and whether the validation evidence supports accepting the run.
