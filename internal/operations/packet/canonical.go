package packet

import (
	"bytes"
	"strconv"
)

func encodeCanonical(document Document) ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	writeFieldName(&out, "schema_version", false)
	writeString(&out, document.SchemaVersion)
	writeFieldName(&out, "created_at", true)
	writeString(&out, document.CreatedAt)
	writeFieldName(&out, "role", true)
	writeString(&out, string(document.Role))
	writeFieldName(&out, "operation_id", true)
	writeString(&out, string(document.OperationID))
	writeFieldName(&out, "surface_contract", true)
	writeString(&out, string(document.SurfaceContract))
	writeFieldName(&out, "surface_manifest_sha256", true)
	writeString(&out, document.SurfaceManifestSHA256)
	if document.PriorPacket != nil {
		writeFieldName(&out, "prior_packet", true)
		writePriorPacket(&out, *document.PriorPacket)
	}
	writeFieldName(&out, "output", true)
	writeOutput(&out, document.Output)
	writeFieldName(&out, "project", true)
	writeProject(&out, document.Project)
	writeFieldName(&out, "workflow_references", true)
	writeWorkflowReferences(&out, document.WorkflowReferences)
	writeFieldName(&out, "attestations", true)
	writeAttestations(&out, document.Attestations)
	writeFieldName(&out, "inputs", true)
	writeInputs(&out, document.Inputs)
	writeFieldName(&out, "repositories", true)
	writeRepositories(&out, document.Repositories)
	writeFieldName(&out, "relay_specs", true)
	writeGovernance(&out, document.RelaySpecs)
	writeFieldName(&out, "manifest_domain", true)
	writeManifestDomain(&out, document.ManifestDomain)
	writeFieldName(&out, "source_policy", true)
	writeString(&out, string(document.SourcePolicy))
	writeFieldName(&out, "historical_authority", true)
	writeString(&out, string(document.HistoricalAuthority))
	writeFieldName(&out, "allowed_actions", true)
	out.WriteByte('[')
	for index, action := range document.AllowedActions {
		if index > 0 {
			out.WriteByte(',')
		}
		writeString(&out, string(action))
	}
	out.WriteByte(']')
	writeFieldName(&out, "readiness_state", true)
	writeString(&out, document.ReadinessState)
	out.WriteByte('}')
	return out.Bytes(), nil
}

func writePriorPacket(out *bytes.Buffer, value PriorPacketIdentity) {
	out.WriteByte('{')
	writeFieldName(out, "packet_id", false)
	writeString(out, value.PacketID)
	writeFieldName(out, "packet_sha256", true)
	writeString(out, value.PacketSHA256)
	out.WriteByte('}')
}

func writeOutput(out *bytes.Buffer, value OutputContract) {
	out.WriteByte('{')
	writeFieldName(out, "output_kind", false)
	writeString(out, value.OutputKind)
	writeFieldName(out, "output_persistence", true)
	writeString(out, value.OutputPersistence)
	out.WriteByte('}')
}

func writeProject(out *bytes.Buffer, value ProjectBinding) {
	out.WriteByte('{')
	writeFieldName(out, "project_id", false)
	writeString(out, value.ProjectID)
	out.WriteByte('}')
}

func writeWorkflowReferences(out *bytes.Buffer, values []WorkflowReference) {
	out.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			out.WriteByte(',')
		}
		writeWorkflowReference(out, value)
	}
	out.WriteByte(']')
}

func writeWorkflowReference(out *bytes.Buffer, value WorkflowReference) {
	out.WriteByte('{')
	writeFieldName(out, "kind", false)
	writeString(out, string(value.Kind))
	switch value.Kind {
	case "plan":
		writeFieldName(out, "plan_id", true)
		writeString(out, value.PlanID)
		writeFieldName(out, "canonical_artifact_id", true)
		writeString(out, value.CanonicalArtifactID)
		writeFieldName(out, "canonical_artifact_sha256", true)
		writeString(out, value.CanonicalArtifactSHA256)
	case "pass":
		writeFieldName(out, "plan_id", true)
		writeString(out, value.PlanID)
		writeFieldName(out, "pass_id", true)
		writeString(out, value.PassID)
		writeFieldName(out, "pass_number", true)
		writeInt(out, value.PassNumber)
	case "run":
		writeFieldName(out, "run_id", true)
		writeString(out, value.RunID)
		writeFieldName(out, "execution_spec_artifact_id", true)
		writeString(out, value.ExecutionSpecArtifactID)
		writeFieldName(out, "execution_spec_sha256", true)
		writeString(out, value.ExecutionSpecSHA256)
	case "audit_packet":
		writeFieldName(out, "run_id", true)
		writeString(out, value.RunID)
		writeFieldName(out, "audit_packet_id", true)
		writeString(out, value.AuditPacketID)
		writeFieldName(out, "audit_packet_sha256", true)
		writeString(out, value.AuditPacketSHA256)
	case "audit_decision":
		writeFieldName(out, "run_id", true)
		writeString(out, value.RunID)
		writeFieldName(out, "audit_decision_id", true)
		writeString(out, value.AuditDecisionID)
		writeFieldName(out, "decision", true)
		writeString(out, value.Decision)
		writeFieldName(out, "recorded_at", true)
		writeString(out, value.RecordedAt)
	}
	out.WriteByte('}')
}

func writeAttestations(out *bytes.Buffer, values []Attestation) {
	out.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			out.WriteByte(',')
		}
		writeAttestation(out, value)
	}
	out.WriteByte(']')
}

func writeAttestation(out *bytes.Buffer, value Attestation) {
	out.WriteByte('{')
	writeFieldName(out, "kind", false)
	writeString(out, string(value.Kind))
	writeFieldName(out, "input_name", true)
	writeString(out, value.InputName)
	switch value.Kind {
	case "confirmed_intent":
		writeFieldName(out, "subject_sha256", true)
		writeString(out, value.SubjectSHA256)
		writeFieldName(out, "confirmed", true)
		writeBool(out, value.Confirmed)
	case "approved_artifact":
		writeFieldName(out, "subject_sha256", true)
		writeString(out, value.SubjectSHA256)
		writeFieldName(out, "approved", true)
		writeBool(out, value.Approved)
	case "candidate_for_review":
		writeFieldName(out, "subject_sha256", true)
		writeString(out, value.SubjectSHA256)
		writeFieldName(out, "complete_transfer", true)
		writeBool(out, value.CompleteTransfer)
	case "execution_mode_selection":
		writeFieldName(out, "selected_mode", true)
		writeString(out, value.SelectedMode)
	case "complete_review_result":
		writeFieldName(out, "subject_sha256", true)
		writeString(out, value.SubjectSHA256)
		writeFieldName(out, "reviewed_candidate_sha256", true)
		writeString(out, value.ReviewedCandidateSHA256)
		writeFieldName(out, "review_result", true)
		writeString(out, value.ReviewResult)
		writeFieldName(out, "complete", true)
		writeBool(out, value.Complete)
	case "completed_dependency_outcomes", "exact_evidence":
		writeFieldName(out, "subject_sha256", true)
		writeString(out, value.SubjectSHA256)
		writeFieldName(out, "complete", true)
		writeBool(out, value.Complete)
	case "operator_confirmation", "separate_session_authorship":
		writeFieldName(out, "confirmed", true)
		writeBool(out, value.Confirmed)
	case "sensitive_data_clearance":
		writeFieldName(out, "clearance", true)
		writeClearance(out, *value.Clearance)
	}
	out.WriteByte('}')
}

func writeClearance(out *bytes.Buffer, value SensitiveDataClearance) {
	out.WriteByte('{')
	writeFieldName(out, "policy_version", false)
	writeString(out, value.PolicyVersion)
	writeFieldName(out, "subject_sha256", true)
	writeString(out, value.SubjectSHA256)
	writeFieldName(out, "declaration", true)
	writeDeclaration(out, value.Declaration)
	writeFieldName(out, "confirmed", true)
	writeBool(out, value.Confirmed)
	out.WriteByte('}')
}

func writeDeclaration(out *bytes.Buffer, value SensitiveDataDeclaration) {
	out.WriteByte('{')
	writeFieldName(out, "password", false)
	writeBool(out, value.Password)
	writeFieldName(out, "api_key_or_access_token", true)
	writeBool(out, value.APIKeyOrAccessToken)
	writeFieldName(out, "refresh_token_or_session_material", true)
	writeBool(out, value.RefreshTokenOrSessionMaterial)
	writeFieldName(out, "cookie_or_authorization_header", true)
	writeBool(out, value.CookieOrAuthorizationHeader)
	writeFieldName(out, "private_or_ssh_key", true)
	writeBool(out, value.PrivateOrSSHKey)
	writeFieldName(out, "credential", true)
	writeBool(out, value.Credential)
	writeFieldName(out, "complete_secret_bearing_environment_file", true)
	writeBool(out, value.CompleteSecretBearingEnvironmentFile)
	writeFieldName(out, "avoidable_signed_secret_bearing_url", true)
	writeBool(out, value.AvoidableSignedSecretBearingURL)
	out.WriteByte('}')
}

func writeInputs(out *bytes.Buffer, values []InputBinding) {
	out.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			out.WriteByte(',')
		}
		writeInput(out, value)
	}
	out.WriteByte(']')
}

func writeInput(out *bytes.Buffer, value InputBinding) {
	out.WriteByte('{')
	writeFieldName(out, "input_name", false)
	writeString(out, value.InputName)
	writeFieldName(out, "input_role", true)
	writeString(out, string(value.InputRole))
	writeFieldName(out, "source_kind", true)
	writeString(out, string(value.SourceKind))
	writeFieldName(out, "display_name", true)
	writeString(out, value.DisplayName)
	writeFieldName(out, "media_type", true)
	writeString(out, value.MediaType)
	writeFieldName(out, "sha256", true)
	writeString(out, value.SHA256)
	writeFieldName(out, "size_bytes", true)
	writeInt(out, value.SizeBytes)
	writeFieldName(out, "attestation_kind", true)
	writeString(out, string(value.AttestationKind))
	writeFieldName(out, "source", true)
	writeInputSource(out, value.Source)
	out.WriteByte('}')
}

func writeInputSource(out *bytes.Buffer, value InputSource) {
	out.WriteByte('{')
	writeFieldName(out, "kind", false)
	writeString(out, string(value.Kind))
	switch value.Kind {
	case InputSourceUploadedFile:
		writeFieldName(out, "file_index", true)
		writeInt(out, value.FileIndex)
		writeFieldName(out, "artifact_id", true)
		writeString(out, value.ArtifactID)
	case InputSourceRelayArtifact, InputSourceInlineText:
		writeFieldName(out, "artifact_id", true)
		writeString(out, value.ArtifactID)
	case InputSourceWorkflowRecord:
		writeFieldName(out, "workflow_reference", true)
		writeWorkflowReference(out, value.WorkflowReference)
		writeFieldName(out, "snapshot_artifact_id", true)
		writeString(out, value.SnapshotArtifactID)
		writeFieldName(out, "snapshot_sha256", true)
		writeString(out, value.SnapshotSHA256)
	case InputSourceCommittedSource:
		writeFieldName(out, "repository_binding_id", true)
		writeString(out, value.RepositoryBindingID)
		writeFieldName(out, "commit_oid", true)
		writeString(out, value.CommitOID)
		writeFieldName(out, "tree_oid", true)
		writeString(out, value.TreeOID)
		writeFieldName(out, "path", true)
		writePathIdentity(out, value.Path)
		writeFieldName(out, "blob_oid", true)
		writeString(out, value.BlobOID)
	}
	out.WriteByte('}')
}

func writeRepositories(out *bytes.Buffer, values []RepositoryBinding) {
	out.WriteByte('[')
	for index, value := range values {
		if index > 0 {
			out.WriteByte(',')
		}
		writeRepository(out, value)
	}
	out.WriteByte(']')
}

func writeRepository(out *bytes.Buffer, value RepositoryBinding) {
	out.WriteByte('{')
	writeFieldName(out, "repository_key", false)
	writeString(out, value.RepositoryKey)
	writeFieldName(out, "repository_target", true)
	writeString(out, value.RepositoryTarget)
	writeFieldName(out, "binding_order", true)
	writeInt(out, value.BindingOrder)
	writeFieldName(out, "revision_source", true)
	writeString(out, value.RevisionSource)
	if value.RevisionSource == RevisionSourceConfiguredWorkingBranch {
		writeFieldName(out, "configured_working_branch_ref", true)
		writeString(out, value.ConfiguredWorkingBranchRef)
	}
	writeFieldName(out, "repository_target_configuration_version", true)
	writeInt(out, value.RepositoryTargetConfigurationVersion)
	writeFieldName(out, "commit_oid", true)
	writeString(out, value.CommitOID)
	writeFieldName(out, "tree_oid", true)
	writeString(out, value.TreeOID)
	writeFieldName(out, "anchors", true)
	out.WriteByte('[')
	for index, anchor := range value.Anchors {
		if index > 0 {
			out.WriteByte(',')
		}
		writeAnchor(out, anchor)
	}
	out.WriteByte(']')
	out.WriteByte('}')
}

func writeGovernance(out *bytes.Buffer, value GovernanceBinding) {
	out.WriteByte('{')
	writeFieldName(out, "repository_key", false)
	writeString(out, value.RepositoryKey)
	writeFieldName(out, "repository_target", true)
	writeString(out, value.RepositoryTarget)
	writeFieldName(out, "reserved", true)
	writeBool(out, value.Reserved)
	writeFieldName(out, "revision_source", true)
	writeString(out, value.RevisionSource)
	if value.RevisionSource == RevisionSourceConfiguredWorkingBranch {
		writeFieldName(out, "configured_working_branch_ref", true)
		writeString(out, value.ConfiguredWorkingBranchRef)
	}
	writeFieldName(out, "repository_target_configuration_version", true)
	writeInt(out, value.RepositoryTargetConfigurationVersion)
	writeFieldName(out, "commit_oid", true)
	writeString(out, value.CommitOID)
	writeFieldName(out, "tree_oid", true)
	writeString(out, value.TreeOID)
	out.WriteByte('}')
}

func writeAnchor(out *bytes.Buffer, value Anchor) {
	out.WriteByte('{')
	writeFieldName(out, "anchor_name", false)
	writeString(out, value.AnchorName)
	writeFieldName(out, "purpose", true)
	writeString(out, string(value.Purpose))
	writeFieldName(out, "commit_oid", true)
	writeString(out, value.CommitOID)
	writeFieldName(out, "tree_oid", true)
	writeString(out, value.TreeOID)
	out.WriteByte('}')
}

func writeManifestDomain(out *bytes.Buffer, value ManifestDomainBinding) {
	out.WriteByte('{')
	writeFieldName(out, "manifest_path", false)
	writePathIdentity(out, value.ManifestPath)
	writeFieldName(out, "manifest_blob_oid", true)
	writeString(out, value.ManifestBlobOID)
	writeFieldName(out, "manifest_sha256", true)
	writeString(out, value.ManifestSHA256)
	writeFieldName(out, "domain", true)
	writeString(out, string(value.Domain))
	writeFieldName(out, "members", true)
	out.WriteByte('[')
	for index, member := range value.Members {
		if index > 0 {
			out.WriteByte(',')
		}
		writeManifestMember(out, member)
	}
	out.WriteByte(']')
	out.WriteByte('}')
}

func writeManifestMember(out *bytes.Buffer, value ManifestMember) {
	out.WriteByte('{')
	writeFieldName(out, "member_order", false)
	writeInt(out, value.MemberOrder)
	writeFieldName(out, "path", true)
	writePathIdentity(out, value.Path)
	writeFieldName(out, "blob_oid", true)
	writeString(out, value.BlobOID)
	writeFieldName(out, "byte_size", true)
	writeInt(out, value.ByteSize)
	writeFieldName(out, "sha256", true)
	writeString(out, value.SHA256)
	out.WriteByte('}')
}

func writePathIdentity(out *bytes.Buffer, value PathIdentity) {
	out.WriteByte('{')
	writeFieldName(out, "path_id", false)
	writeString(out, value.PathID)
	writeFieldName(out, "byte_length", true)
	writeInt(out, value.ByteLength)
	if value.PathBytesBase64 != "" {
		writeFieldName(out, "path_bytes_base64", true)
		writeString(out, value.PathBytesBase64)
	}
	out.WriteByte('}')
}

func writeFieldName(out *bytes.Buffer, name string, comma bool) {
	if comma {
		out.WriteByte(',')
	}
	writeString(out, name)
	out.WriteByte(':')
}

func writeString(out *bytes.Buffer, value string) {
	out.WriteByte('"')
	for _, char := range value {
		switch char {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		default:
			if char >= 0 && char <= 0x1f {
				const hex = "0123456789abcdef"
				out.WriteString(`\u00`)
				out.WriteByte(hex[byte(char)>>4])
				out.WriteByte(hex[byte(char)&0x0f])
			} else {
				out.WriteRune(char)
			}
		}
	}
	out.WriteByte('"')
}

func writeInt(out *bytes.Buffer, value int64) {
	out.WriteString(strconv.FormatInt(value, 10))
}

func writeBool(out *bytes.Buffer, value bool) {
	if value {
		out.WriteString("true")
		return
	}
	out.WriteString("false")
}
