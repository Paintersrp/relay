export interface ProgramMember { id: string; packageId: string; runId: string; assignmentArtifactId: string; repoTarget: string; branch: string; baseCommit: string; state: string; outcome: string; resultBranch: string; branchHeadSha: string; blocker: string; ticketRevisionRowId: number; }
export interface ProgramDispatch { id: string; workspaceId: string; repoTarget: string; branch: string; baseCommit: string; status: string; laterIntegrationRisks: string; members: ProgramMember[]; }
export interface ProgramHandoffMember { sequence: number; memberId: string; ticketId: string; ticketRevision: number; packageId: string; runId: string; assignmentArtifactId: string; assignmentSha256: string; assignment: unknown; repoTarget: string; branch: string; baseCommit: string; }
export interface ProgramHandoff { dispatchId: string; workspaceId: string; repoTarget: string; branch: string; baseCommit: string; members: ProgramHandoffMember[]; }
export interface ProgramHandoffResult { handoff: ProgramHandoff; text: string; }

/** One immutable Relay-generated Integration Assignment of a Program dispatch
 * with its exact byte-verified transport document. The assignment is runtime
 * transport only: it carries no Delivery Plan identity or authority and is
 * never patched or reused after failure. */
export interface ProgramIntegrationAssignment {
  assignmentId: string;
  dispatchId: string;
  workspaceId: string;
  repoTarget: string;
  branch: string;
  baseCommit: string;
  status: "generated" | "admitted" | "verified" | "failed";
  contentSha256: string;
  document: ProgramIntegrationAssignmentDocument;
}

export interface ProgramIntegrationAssignmentDocument {
  schemaVersion: string;
  assignment: { assignmentId: string; dispatchId: string; repoTarget: string; branch: string; baseCommit: string };
  constituents: ProgramIntegrationAssignmentConstituent[];
  combinedValidation: ProgramIntegrationCombinedValidation[];
  requiredEvidence: ProgramIntegrationRequiredEvidenceItem[];
}

export interface ProgramIntegrationAssignmentConstituent {
  sequence: number;
  memberId: string;
  ticketId: string;
  ticketRevision: number;
  acceptedCommit: string;
  pushedBranch: string;
  packageId: string;
  runId: string;
  executionAssignment: { artifactId: string; sha256: string };
  auditDecisionId: string;
  eligibilityId: string;
  sharedDesign: { requiredInvariants: string[]; forbiddenBehaviors: string[]; dependsOn: { ticketId: string; revision: number }[] };
  validationCommands: ProgramIntegrationValidationCommand[];
  requiredEvidence: ProgramIntegrationRequiredEvidence[];
}

export interface ProgramIntegrationValidationCommand { workingDirectory: string; command: string; expected: string }
export interface ProgramIntegrationRequiredEvidence { kind: string; obligation: string }
export interface ProgramIntegrationCombinedValidation { sequence: number; constituentSequence: number; workingDirectory: string; command: string; expected: string }
export interface ProgramIntegrationRequiredEvidenceItem { sequence: number; constituentSequence: number; kind: string; obligation: string }

/** The one admitted external Merge result of an Assignment. The outcomes are
 * exactly the bound combined validation commands and required evidence; the
 * admitted result is immutable evidence. */
export interface ProgramIntegrationMergeResult {
  resultId: string;
  assignmentId: string;
  dispatchId: string;
  integratedCommit: string;
  preservationIdentity: string;
  conflictResolution: "clean" | "mechanically_resolved" | "material_conflict";
  conflictEvidence: string;
  validations: ProgramIntegrationValidationOutcome[];
  evidence: ProgramIntegrationEvidenceOutcome[];
}

export interface ProgramIntegrationValidationOutcome { sequence: number; constituentSequence: number; command: string; expected: string; status: "passed" | "failed"; evidence: string }
export interface ProgramIntegrationEvidenceOutcome { sequence: number; constituentSequence: number; kind: string; obligation: string; status: "passed" | "failed"; evidence: string }

/** Relay's recorded post-Merge verification of one admitted Merge result. A
 * successful pass is the only basis for the ordinary completed outcome of each
 * bound constituent; a failed verification records immutable failure evidence
 * and creates no completion. */
export interface ProgramIntegrationVerification {
  verificationId: string;
  assignmentId: string;
  dispatchId: string;
  outcome: "passed" | "failed";
  failureReason: string;
  completed: ProgramIntegrationCompletion[];
}

export interface ProgramIntegrationCompletion { memberId: string; ticketId: string; ticketRevision: number; completed: boolean }

/** The recorded failed verification of an Assignment. A passed verification or
 * the absence of verification is not a failure. */
export interface ProgramIntegrationFailure {
  verificationId: string;
  assignmentId: string;
  dispatchId: string;
  failureReason: string;
}

export interface GenerateIntegrationAssignmentRequest { expectedVersion: number; memberIds: string[] }
export interface MergeOutcomeInput { command: string; expected: string; status: "passed" | "failed"; evidence: string }
export interface EvidenceOutcomeInput { kind: string; obligation: string; status: "passed" | "failed"; evidence: string }
export interface AdmitIntegrationMergeResultRequest {
  expectedVersion: number;
  integratedCommit: string;
  preservationIdentity: string;
  conflictResolution: "clean" | "mechanically_resolved" | "material_conflict";
  conflictEvidence: string;
  validations: MergeOutcomeInput[];
  evidence: EvidenceOutcomeInput[];
}
export interface ProgramIntegrationAssignmentResult { assignment: ProgramIntegrationAssignment; text: string }
