// Package runs owns run service, use-case, and run lifecycle workflow code for
// the runs feature. It loads run detail data, derives presentation-neutral run
// views, and performs run lifecycle operations (intake approval, prepare,
// render, execute, validate, repair) by delegating to the existing workflow
// packages.
//
// It must not import internal/api or internal/api/<feature>.
package runs
