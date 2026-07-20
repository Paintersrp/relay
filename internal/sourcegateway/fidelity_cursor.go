package sourcegateway

import "relay/internal/app/operations"

func fidelityCursorBase(a operations.SourceReadAuthority, kind, fingerprint string) cursorPayload {
	return cursorPayload{Version: CursorVersion, Kind: kind, PacketID: a.Summary.PacketID, PacketSHA256: a.Summary.PacketSHA256, SurfaceContract: string(a.Summary.SurfaceContract), OperationID: string(a.Summary.OperationID), ProjectID: a.Summary.ProjectID, RepositoryKey: a.RepositoryKey, DependencyKey: a.DependencyKey, AnchorName: a.AnchorName, PublicationID: a.PublicationID, VaultRelationshipRowID: a.Relationship.ID, CommitOID: a.Relationship.CommitOID, TreeOID: a.Relationship.TreeOID, RequestFingerprint: fingerprint}
}
func fidelityPairCursorBase(before, after operations.SourceReadAuthority, kind, fingerprint string) cursorPayload {
	v := fidelityCursorBase(before, kind, fingerprint)
	v.SecondaryDependencyKey = after.DependencyKey
	v.SecondaryAnchorName = after.AnchorName
	v.SecondaryPublicationID = after.PublicationID
	v.SecondaryVaultRelationshipRowID = after.Relationship.ID
	v.SecondaryCommitOID = after.Relationship.CommitOID
	v.SecondaryTreeOID = after.Relationship.TreeOID
	return v
}
func fidelityCursorMatches(v cursorPayload, a operations.SourceReadAuthority, kind, fingerprint string) bool {
	return fidelityCursorMatchesPrimary(v, a, kind, fingerprint) && v.SecondaryDependencyKey == "" && v.SecondaryAnchorName == "" && v.SecondaryPublicationID == "" && v.SecondaryVaultRelationshipRowID == 0 && v.SecondaryCommitOID == "" && v.SecondaryTreeOID == ""
}
func fidelityCursorMatchesPrimary(v cursorPayload, a operations.SourceReadAuthority, kind, fingerprint string) bool {
	return v.Version == CursorVersion && v.Kind == kind && v.PacketID == a.Summary.PacketID && v.PacketSHA256 == a.Summary.PacketSHA256 && v.SurfaceContract == string(a.Summary.SurfaceContract) && v.OperationID == string(a.Summary.OperationID) && v.ProjectID == a.Summary.ProjectID && v.RepositoryKey == a.RepositoryKey && v.DependencyKey == a.DependencyKey && v.AnchorName == a.AnchorName && v.PublicationID == a.PublicationID && v.VaultRelationshipRowID == a.Relationship.ID && v.CommitOID == a.Relationship.CommitOID && v.TreeOID == a.Relationship.TreeOID && v.RequestFingerprint == fingerprint
}
func fidelityPairCursorMatches(v cursorPayload, before, after operations.SourceReadAuthority, kind, fingerprint string) bool {
	return fidelityCursorMatchesPrimary(v, before, kind, fingerprint) && v.SecondaryDependencyKey == after.DependencyKey && v.SecondaryAnchorName == after.AnchorName && v.SecondaryPublicationID == after.PublicationID && v.SecondaryVaultRelationshipRowID == after.Relationship.ID && v.SecondaryCommitOID == after.Relationship.CommitOID && v.SecondaryTreeOID == after.Relationship.TreeOID
}
