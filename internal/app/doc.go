// Package app documents Relay backend application-layer ownership.
//
// Import direction:
//   - internal/api/<feature> may import internal/app/<feature>.
//   - internal/app/<feature> may import internal/store and infrastructure packages.
//   - internal/app/<feature> must not import internal/api or internal/api/<feature>.
//   - internal/store must not import internal/api or internal/app.
//
// PASS-001 creates this skeleton only. Feature service migrations are out of
// scope until their selected passes.
package app
