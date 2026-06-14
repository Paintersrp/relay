package handlers

import "testing"

func TestClassifyOpenCodeLifecycleDiagnostic(t *testing.T) {
	tests := []struct {
		name string
		diag openCodeLifecycleDiagnostic
		want string
	}{
		{
			name: "process alive waiting",
			diag: openCodeLifecycleDiagnostic{
				CommandStartReturnedAt: "2026-06-14T12:00:00Z",
				WaitStartedAt:          "2026-06-14T12:00:00Z",
				ProcessAlive:           boolPtr(true),
			},
			want: "process_alive_waiting",
		},
		{
			name: "process exited wait blocked",
			diag: openCodeLifecycleDiagnostic{
				CommandStartReturnedAt: "2026-06-14T12:00:00Z",
				WaitStartedAt:          "2026-06-14T12:00:00Z",
				ProcessAlive:           boolPtr(false),
			},
			want: "process_exited_wait_blocked",
		},
		{
			name: "wait returned finalize missing",
			diag: openCodeLifecycleDiagnostic{
				CommandStartReturnedAt: "2026-06-14T12:00:00Z",
				WaitStartedAt:          "2026-06-14T12:00:01Z",
				WaitReturnedAt:         "2026-06-14T12:00:02Z",
			},
			want: "wait_returned_finalize_missing",
		},
		{
			name: "finalized ui stale",
			diag: openCodeLifecycleDiagnostic{
				CommandStartReturnedAt: "2026-06-14T12:00:00Z",
				WaitStartedAt:          "2026-06-14T12:00:01Z",
				WaitReturnedAt:         "2026-06-14T12:00:02Z",
				StoreFinalizeEndedAt:   "2026-06-14T12:00:03Z",
				LatestStoreFinishedAt:  "2026-06-14T12:00:03Z",
				LastLifecycleState:     "running_no_output",
			},
			want: "finalized_ui_stale",
		},
		{
			name: "completed",
			diag: openCodeLifecycleDiagnostic{
				CommandStartReturnedAt: "2026-06-14T12:00:00Z",
				WaitStartedAt:          "2026-06-14T12:00:01Z",
				WaitReturnedAt:         "2026-06-14T12:00:02Z",
				StoreFinalizeEndedAt:   "2026-06-14T12:00:03Z",
				LatestStoreFinishedAt:  "2026-06-14T12:00:03Z",
				LastLifecycleState:     "completed",
			},
			want: "completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := classifyOpenCodeLifecycleDiagnostic(tt.diag)
			if got != tt.want {
				t.Fatalf("classifyOpenCodeLifecycleDiagnostic() = %q, want %q", got, tt.want)
			}
		})
	}
}

func boolPtr(v bool) *bool {
	return &v
}
