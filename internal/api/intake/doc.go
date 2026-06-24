// Package intake is the planner-handoff intake feature HTTP transport adapter.
//
// It owns the intake JSON API route, request/response DTOs, and response
// assembly. It delegates run creation behavior to relay/internal/app/intake and
// reuses run presentation via relay/internal/api/runs. It must not import root
// relay/internal/api or the store package directly.
package intake
