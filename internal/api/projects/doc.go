// Package projects is the project feature HTTP transport adapter.
//
// It owns project and project-repository API DTOs, request/response mappers,
// the validation error response helper, HTTP handlers, and the project route
// mounter. It delegates all business behavior to relay/internal/app/projects
// and must not import root relay/internal/api or perform persistence directly.
package projects
