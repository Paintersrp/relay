// Package projects is a temporary compatibility shim for the migrated project
// service package. The project service, use-case, and validation code now lives
// in relay/internal/app/projects (PASS-002). This package re-exports the app
// package symbols so unmigrated callers continue to compile during the
// transition. It must contain aliases only — no business logic and no store
// access.
package projects

import app "relay/internal/app/projects"

const (
	RepositoryRolePrimary    = app.RepositoryRolePrimary
	RepositoryRoleReference  = app.RepositoryRoleReference
	RepositoryRoleContracts  = app.RepositoryRoleContracts
	RepositoryRoleDocs       = app.RepositoryRoleDocs
	ProjectStatusActive      = app.ProjectStatusActive
	ProjectStatusArchived    = app.ProjectStatusArchived
	DefaultBranch            = app.DefaultBranch
	DefaultMaxFileSizeBytes  = app.DefaultMaxFileSizeBytes
	MinMaxFileSizeBytes      = app.MinMaxFileSizeBytes
	MaxAllowedFileSizeBytes  = app.MaxAllowedFileSizeBytes
	DefaultListProjectsLimit = app.DefaultListProjectsLimit
)

type (
	Service                          = app.Service
	ProjectInput                     = app.ProjectInput
	ProjectRepositoryInput           = app.ProjectRepositoryInput
	ProjectValidationIssue           = app.ProjectValidationIssue
	NormalizedProjectInput           = app.NormalizedProjectInput
	NormalizedProjectRepositoryInput = app.NormalizedProjectRepositoryInput
)

var (
	NewService                      = app.NewService
	NormalizeProjectInput           = app.NormalizeProjectInput
	NormalizeProjectRepositoryInput = app.NormalizeProjectRepositoryInput
	ValidateProjectInput            = app.ValidateProjectInput
	ValidateProjectRepositoryInput  = app.ValidateProjectRepositoryInput
)
