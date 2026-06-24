// Package plans owns plan service, use-case, and business workflow code
// for the plans feature. It is the single implementation owner of plan
// validation, submission, lifecycle synchronization, query, and orchestrator
// work-packet behavior.
//
// It must not import internal/api or internal/api/<feature>.
package plans
