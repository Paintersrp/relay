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

func (m *Manager) ReadRetainedTree(ctx context.Context, request ReadRetainedTreeRequest) (ReadRetainedTreeResult, error) {
	if m == nil || !validOID(request.TreeOID) {
		return ReadRetainedTreeResult{}, &Error{Code: CodeInvalidRequest}
	}
	var result ReadRetainedTreeResult
	err := m.withActiveRetentionEdge(ctx, request.Relationship, func(vaultPath string, _ workflowstore.SourceVaultClosure) error {
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
	err := m.withActiveRetentionEdge(ctx, request.Relationship, func(vaultPath string, _ workflowstore.SourceVaultClosure) error {
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
