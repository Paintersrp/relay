// Package artifacts is the run artifact feature HTTP transport adapter.
//
// It owns run artifact list/content JSON API routes and presentation, reusing
// the run feature DTOs and presenter. It delegates all data and file access to
// relay/internal/app/runs and must not import root relay/internal/api, the
// store package, or read files directly.
package artifacts
