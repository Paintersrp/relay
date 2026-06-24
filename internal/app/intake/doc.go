// Package intake owns intake service/use-case access for the intake feature.
// It encapsulates planner-handoff run creation behavior (durable run creation,
// provenance, artifact writes, validation checks, and managed pass status
// transitions) and returns transport-mappable typed errors.
//
// It must not import internal/api or internal/api/<feature>.
package intake
