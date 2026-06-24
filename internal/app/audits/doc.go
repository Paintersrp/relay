// Package audits owns the audit use-case, service, and business workflow code
// for the audits feature.
//
// It provides orchestration for local audits, run audit status, audit packet
// generation, audit decision submission, and run closeout.
//
// It must not import internal/api or internal/api/<feature>.
package audits
