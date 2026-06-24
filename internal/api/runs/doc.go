// Package runs is the run feature HTTP transport adapter.
//
// It owns run and run-lifecycle JSON API routes, run/artifact/event response
// DTOs, run presentation mapping, and the run route mounter. It delegates all
// business behavior to relay/internal/app/runs and relay/internal/app/plans and
// must not import root relay/internal/api or the store package directly.
package runs
