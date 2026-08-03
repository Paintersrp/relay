package sourcevault

import (
	"context"

	workflowstore "relay/internal/store/workflow"
)

type RetainedTreeEntry struct {
	Name       []byte
	Mode       string
	ObjectType string
	ObjectOID  string
}

type ReadRetainedTreeRequest struct {
	Relationship workflowstore.OperationPacketVaultRelationship
	TreeOID      string
}

type ReadRetainedTreeResult struct {
	TreeOID string
	Entries []RetainedTreeEntry
}

type ReadRetainedBlobRangeRequest struct {
	Relationship workflowstore.OperationPacketVaultRelationship
	BlobOID      string
	Offset       int64
	Limit        int64
}

type ReadRetainedBlobRangeResult struct {
	BlobOID   string
	Offset    int64
	TotalSize int64
	Bytes     []byte
}

// RetainedReadSession holds one verified retention edge and vault lock for a
// single gateway operation.
type RetainedReadSession interface {
	ReadRetainedTree(context.Context, ReadRetainedTreeRequest) (ReadRetainedTreeResult, error)
	ReadRetainedBlobRange(context.Context, ReadRetainedBlobRangeRequest) (ReadRetainedBlobRangeResult, error)
	Close() error
}

type retainedReadSession struct {
	manager   *Manager
	vaultPath string
	unlock    func()
	trees     map[string][]RetainedTreeEntry
}

func (m *Manager) OpenRetainedReadSession(ctx context.Context, relationship workflowstore.OperationPacketVaultRelationship) (RetainedReadSession, error) {
	if m == nil {
		return nil, &Error{Code: CodeInvalidRequest}
	}
	retention, err := m.store.GetSourceVaultRetentionByRowID(ctx, relationship.RetentionRowID)
	if err != nil {
		return nil, managerError(ctx, err, CodeDatabaseFailure)
	}
	if !retentionMatchesRelationship(retention, relationship) {
		return nil, &Error{Code: CodeVaultUnavailable}
	}
	closure, err := m.store.GetSourceVaultClosureByRowID(ctx, relationship.ClosureRowID)
	if err != nil {
		return nil, managerError(ctx, err, CodeDatabaseFailure)
	}
	if !closureMatchesRelationship(closure, relationship) {
		return nil, &Error{Code: CodeVaultUnavailable}
	}
	vault, err := m.store.GetSourceVaultByRowID(ctx, relationship.VaultRowID)
	if err != nil {
		return nil, managerError(ctx, err, CodeDatabaseFailure)
	}
	if vault.ID != closure.VaultRowID {
		return nil, &Error{Code: CodeVaultUnavailable}
	}
	unlock := m.lockVault(vault.VaultID)
	path, err := m.git.VaultPath(vault.RelativePath)
	if err == nil {
		err = m.git.ValidateVault(ctx, path)
	}
	if err == nil {
		err = m.git.VerifyVaultClosure(ctx, path, closure.CommitOID, closure.TreeOID, closure.RefName)
	}
	if err != nil {
		unlock()
		return nil, managerError(ctx, err, CodeVaultUnavailable)
	}
	return &retainedReadSession{manager: m, vaultPath: path, unlock: unlock, trees: make(map[string][]RetainedTreeEntry)}, nil
}

func (s *retainedReadSession) Close() error {
	if s != nil && s.unlock != nil {
		s.unlock()
		s.unlock = nil
	}
	return nil
}

func (s *retainedReadSession) ReadRetainedTree(ctx context.Context, request ReadRetainedTreeRequest) (ReadRetainedTreeResult, error) {
	if s == nil || s.unlock == nil || !validOID(request.TreeOID) {
		return ReadRetainedTreeResult{}, &Error{Code: CodeInvalidRequest}
	}
	if entries, ok := s.trees[request.TreeOID]; ok {
		return ReadRetainedTreeResult{TreeOID: request.TreeOID, Entries: cloneTreeEntries(entries)}, nil
	}
	entries, err := s.manager.git.ReadTree(ctx, s.vaultPath, request.TreeOID)
	if err != nil {
		return ReadRetainedTreeResult{}, managerError(ctx, err, CodeObjectUnavailable)
	}
	s.trees[request.TreeOID] = cloneTreeEntries(entries)
	return ReadRetainedTreeResult{TreeOID: request.TreeOID, Entries: cloneTreeEntries(entries)}, nil
}

func (s *retainedReadSession) ReadRetainedBlobRange(ctx context.Context, request ReadRetainedBlobRangeRequest) (ReadRetainedBlobRangeResult, error) {
	if s == nil || s.unlock == nil || !validOID(request.BlobOID) || request.Offset < 0 || request.Limit <= 0 {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	value, err := s.manager.git.ReadBlobRange(ctx, s.vaultPath, request.BlobOID, request.Offset, request.Limit)
	if err != nil {
		return ReadRetainedBlobRangeResult{}, managerError(ctx, err, CodeObjectUnavailable)
	}
	return value, nil
}

func (m *Manager) ReadRetainedTree(ctx context.Context, request ReadRetainedTreeRequest) (ReadRetainedTreeResult, error) {
	if m == nil || !validOID(request.TreeOID) {
		return ReadRetainedTreeResult{}, &Error{Code: CodeInvalidRequest}
	}
	var result ReadRetainedTreeResult
	err := m.withActiveRetentionEdge(ctx, request.Relationship, func(vaultPath string, _ workflowstore.SourceVault, _ workflowstore.SourceVaultClosure) error {
		entries, err := m.git.ReadTree(ctx, vaultPath, request.TreeOID)
		if err != nil {
			return managerError(ctx, err, CodeObjectUnavailable)
		}
		result = ReadRetainedTreeResult{TreeOID: request.TreeOID, Entries: cloneTreeEntries(entries)}
		return nil
	})
	if err != nil {
		return ReadRetainedTreeResult{}, err
	}
	return result, nil
}

func (m *Manager) ReadRetainedBlobRange(ctx context.Context, request ReadRetainedBlobRangeRequest) (ReadRetainedBlobRangeResult, error) {
	if m == nil || !validOID(request.BlobOID) || request.Offset < 0 || request.Limit <= 0 {
		return ReadRetainedBlobRangeResult{}, &Error{Code: CodeInvalidRequest}
	}
	var result ReadRetainedBlobRangeResult
	err := m.withActiveRetentionEdge(ctx, request.Relationship, func(vaultPath string, _ workflowstore.SourceVault, _ workflowstore.SourceVaultClosure) error {
		value, err := m.git.ReadBlobRange(ctx, vaultPath, request.BlobOID, request.Offset, request.Limit)
		if err != nil {
			return managerError(ctx, err, CodeObjectUnavailable)
		}
		result = ReadRetainedBlobRangeResult{BlobOID: request.BlobOID, Offset: value.Offset, TotalSize: value.TotalSize, Bytes: append([]byte(nil), value.Bytes...)}
		return nil
	})
	if err != nil {
		return ReadRetainedBlobRangeResult{}, err
	}
	return result, nil
}

func cloneTreeEntries(values []RetainedTreeEntry) []RetainedTreeEntry {
	result := make([]RetainedTreeEntry, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Name = append([]byte(nil), value.Name...)
	}
	return result
}
