package operations

import (
	"fmt"

	"relay/internal/mcp/fileacquisition"
	workflowrepos "relay/internal/repos/workflow"
	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func NewDefaultLifecycleService(store *workflowstore.Store, repositories *workflowrepos.Registry, vaults *sourcevault.Manager, publications *AuthorityPublicationService, fetcher fileacquisition.FetchOne, packetReader *Service) (*LifecycleService, error) {
	if store == nil || repositories == nil || vaults == nil || publications == nil || fetcher == nil || packetReader == nil {
		return nil, fmt.Errorf("complete operation packet lifecycle dependencies are required")
	}
	return NewLifecycleService(LifecycleDependencies{Store: store, Repositories: repositories, Vaults: vaults, Publications: publications, FileFetcher: fetcher, PacketReader: packetReader, IDs: defaultIDGenerator{}, Clock: systemClock{}})
}
