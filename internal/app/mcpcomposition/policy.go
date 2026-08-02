// Package mcpcomposition owns the application composition of retained-source
// policy. Transport packages receive only the resulting route dependencies.
package mcpcomposition

import (
	"context"
	"fmt"

	appoperations "relay/internal/app/operations"
	"relay/internal/mcp/fileacquisition"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcegateway"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

type Services struct {
	Vaults       *sourcevault.Manager
	Publications *appoperations.AuthorityPublicationService
	Packets      *appoperations.Service
	Lifecycle    *appoperations.LifecycleService
	Source       *sourcegateway.Service
}

// Open constructs the retained-source policy seam for integration callers
// that do not otherwise own a vault manager.
func Open(ctx context.Context, vaultRoot string, store *workflowstore.Store, cursorKey []byte, fetcher fileacquisition.FetchOne, options ...sourcegateway.Option) (Services, error) {
	vaults, err := sourcevault.Open(ctx, vaultRoot, store)
	if err != nil {
		return Services{}, err
	}
	publications, err := appoperations.NewAuthorityPublicationService(store, vaults)
	if err != nil {
		return Services{}, err
	}
	return New(store, vaults, publications, cursorKey, fetcher, options...)
}

// New composes packet lifecycle and source reads around one retained-vault
// authority. Callers that reconcile authority at process startup pass that
// same publication service here.
func New(store *workflowstore.Store, vaults *sourcevault.Manager, publications *appoperations.AuthorityPublicationService, cursorKey []byte, fetcher fileacquisition.FetchOne, options ...sourcegateway.Option) (Services, error) {
	if store == nil || vaults == nil || publications == nil || fetcher == nil {
		return Services{}, fmt.Errorf("complete retained-source policy dependencies are required")
	}
	repositories, err := workflowrepos.NewRegistry(store)
	if err != nil {
		return Services{}, err
	}
	packets, err := appoperations.NewService(store)
	if err != nil {
		return Services{}, err
	}
	lifecycle, err := appoperations.NewDefaultLifecycleService(store, repositories, vaults, publications, fetcher, packets)
	if err != nil {
		return Services{}, err
	}
	codec, err := sourcegateway.NewHMACCursorCodec(cursorKey)
	if err != nil {
		return Services{}, err
	}
	source, err := sourcegateway.NewService(packets, vaults, store, codec, options...)
	if err != nil {
		return Services{}, err
	}
	return Services{Vaults: vaults, Publications: publications, Packets: packets, Lifecycle: lifecycle, Source: source}, nil
}

const SourcePathIdentityVersion = sourcegateway.PathIdentityVersion

const (
	MaxInlinePathBytes    = sourcegateway.MaxInlinePathBytes
	MaxTreePageEntries    = sourcegateway.MaxTreePageEntries
	MaxCursorTokenBytes   = sourcegateway.MaxCursorTokenBytes
	MaxSearchPageMatches  = sourcegateway.MaxSearchPageMatches
	MaxSearchLiteralBytes = sourcegateway.MaxSearchLiteralBytes
	MinTextPageBytes      = sourcegateway.MinTextPageBytes
	MaxTextPageBytes      = sourcegateway.MaxTextPageBytes
)
