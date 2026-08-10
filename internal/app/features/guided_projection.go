package features

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	apptickets "relay/internal/app/tickets"
	"relay/internal/guidedapp"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

var (
	ErrGuidedActionBlocked           = errors.New("guided action is not the presently enabled primary action")
	ErrGuidedPackageOwnerUnavailable = errors.New("guided package owner is unavailable")
	ErrGuidedAuditOwnerUnavailable   = errors.New("guided audit owner is unavailable")
	ErrGuidedTicketOwnerUnavailable  = errors.New("guided ticket owner is unavailable")
)

// GuidedTicketOwner is the ticket-owner surface the guided Feature journey
// consumes. It is bound to the exact ticket Service instance the server
// constructs so guided delivery reads and dispatches observe the same instance.
// The Ticket Design Brief is no longer an authority surface: the selected
// approved Delivery Ticket is the sole ticket semantic authority, so the brief
// author/review/approve lifecycle is not part of this contract.
type GuidedTicketOwner interface {
	ListFrontier(context.Context, string) (apptickets.Frontier, error)
	ReadWorkspaceSelection(context.Context, string) (apptickets.WorkspaceSelection, error)
	Read(context.Context, string) (apptickets.TicketDetail, error)
	Select(context.Context, apptickets.SelectInput) (apptickets.SelectionResult, error)
	PromoteApprovedDeliveryTicketCandidate(context.Context, apptickets.CandidateProductionInput) (apptickets.CandidateProductionResult, error)
}

// Guided operation identities transferred by the journey. These are the exact
// established operation IDs; the journey never substitutes a ticket canonical
// JSON artifact or a generic planner envelope for the named operation surface.
const (
	plannerRequirementsOperation              = "planner.requirements"
	plannerSharedDesignOperation              = "planner.shared_design"
	plannerDeliveryTicketOperation            = "planner.delivery_ticket"
	plannerDeliveryTicketRemediationOperation = "planner.delivery_ticket_remediation"
	auditorRequirementsReviewOperation        = "auditor.requirements_review"
	auditorSharedDesignReviewOperation        = "auditor.shared_design_review"
	auditorDeliveryTicketReviewOperation      = "auditor.delivery_ticket_review"
)

// GuidedFeatureProjection is the application-owned semantic journey. It is
// deliberately free of artifact, revision, approval, candidate, package, Run,
// and digest identities; those are resolved by the owning services.
type GuidedFeatureProjection struct {
	Workspace        GuidedWorkspaceSection
	Project          GuidedProjectSection
	Discovery        GuidedDiscoverySection
	Currentness      GuidedCurrentnessSection
	Authority        GuidedAuthoritySection
	Planning         GuidedPlanningSection
	Delivery         GuidedDeliverySection
	Prototype        GuidedPrototypeSection
	Completion       GuidedCompletionSection
	Recovery         GuidedRecoverySection
	Diagnostics      GuidedDiagnosticsSection
	AvailableActions []GuidedFeatureActionAvailability
	PrimaryAction    GuidedFeatureActionAvailability
	Handoff          *GuidedHandoff
}

type GuidedWorkspaceSection struct {
	WorkspaceID string
	FeatureSlug string
	State       string
	Version     int64
	CreatedAt   string
	UpdatedAt   string
}
type GuidedProjectSection struct{ ProjectID, Name string }
type GuidedDiscoverySection struct {
	State, Destination, Rationale, Continuation, Currentness, Basis, ReopenState                             string
	HasCurrentRevision                                                                                       bool
	Blockers, RestorationActions, PendingIntegrations, ActiveOperations, RouteMaterialOpen, RequiredEvidence []string
}
type GuidedCurrentnessSection struct {
	Readiness, Owner, BlockedOperation, Effect, RecoveryCategory string
}

type GuidedAuthoritySection struct {
	CurrentRevisionNumber int
	Layers                []string
}
type GuidedPlanningSection struct {
	Status, CandidateState, ReviewState, ApprovalState, PromotionState                           string
	CandidateCount, AwaitingReview, AwaitingApproval, AwaitingPromotion, NeedsRevision, Promoted int
	HistoricalCount                                                                              int
	Requirements, SharedDesign, DeliveryTicket                                                   GuidedPlanningFamilySection
}

// GuidedPlanningFamilySection carries the semantic progression of one planning
// family so the guided decision can sequence author -> review -> approval ->
// promotion -> next family or ticket without relying on authority presence.
type GuidedPlanningFamilySection struct {
	Count, AwaitingReview, AwaitingApproval, AwaitingPromotion, NeedsRevision, Promoted, Produced int
	State                                                                                         string // none | admitted | needs_revision | reviewed | approved | promoted | produced
}

// GuidedFrontierEntry is the delivery-owner semantic frontier identity. It
// carries only the public ticket identity and revision number resolved by the
// delivery owner; no row identifiers or digests.
type GuidedFrontierEntry struct {
	TicketID         string
	RevisionNumber   int64
	ExternalPriority int64
	RepoTarget       string
	Branch           string
}

// GuidedDeliverySection is the delivery-owner semantic read state consumed by
// the guided decision. Frontier, selection, package, Run, audit, and
// remediation states are composed from the tickets, packages, and audits
// owners; the Feature layer never re-derives lifecycle strings from rows.
type GuidedDeliverySection struct {
	Frontier         []GuidedFrontierEntry
	SelectionState   string // none | active | consumed | superseded
	PackageState     string // none | prepared | approved
	PackageID        string
	RunState         string // none | created | setup_ready | executing | validating | audit_ready | needs_revision | completed | ...
	RunID            string
	AuditState       string // none | awaiting_audit | packet_recorded | decision_recorded
	AuditPacketID    string
	RemediationState string // none | open | reopened
	Diagnostics      []string
}

type GuidedPrototypeSection struct {
	RunState, CleanupState, QAState, EvidenceState, ProcessOutcome string
	RunID                                                          string
	Diagnostics                                                    []string
}
type GuidedCompletionSection struct {
	Gates    []GuidedCompletionGate
	Ready    bool
	Recorded bool
	// Decision is the server-projected closing decision of the current
	// immutable decision row: "completed" or "abandoned". It is empty when no
	// current decision is recorded. Clients display this outcome; they never
	// infer it from lifecycle state.
	Decision string
}
type GuidedRecoverySection struct {
	State, Category string
	Available       []string
}
type GuidedDiagnosticsSection struct {
	Stale, Historical, Discovery, Delivery, Prototype []string
	Integrity                                         GuidedIntegritySection
}

// GuidedIntegritySection is a read-only, owner-derived identity inspection
// surface. It is intentionally separate from progression state and is never
// accepted by GuidedActionInput. Historical evidence is inspectable here but
// is never progression authority.
type GuidedIntegritySection struct {
	Discovery   GuidedIntegrityDiscoverySection
	Authority   []GuidedIntegrityAuthorityRevision
	Planning    []GuidedIntegrityPlanningCandidate
	Delivery    GuidedIntegrityDeliverySection
	Prototype   *GuidedIntegrityPrototypeSection
	Diagnostics []GuidedIntegrityDiagnostic
}

// GuidedIntegrityDiagnostic reports a degraded inspection source without
// converting normal absence or historical evidence into an error. It is local
// to the read-only integrity surface and is never accepted by an action.
type GuidedIntegrityDiagnostic struct {
	Domain    string // discovery | authority | planning | delivery | prototype
	Condition string // unavailable | unreadable | inconsistent | unverifiable
}

// GuidedIntegrityDiscoverySection carries the current discovery revision and
// closure packet plus the full revision history and reopen replacement
// linkage.
type GuidedIntegrityDiscoverySection struct {
	CurrentRevisionID string                        // AC10 current discovery revision ID
	CurrentPacket     *GuidedIntegrityClosurePacket // AC11 current closure packet ID+digest
	History           []GuidedIntegrityDiscoveryRevision
	ReopenEvents      []GuidedIntegrityReopenEvent
}

// GuidedIntegrityClosurePacket is the public closure packet identity and its
// manifest digest.
type GuidedIntegrityClosurePacket struct {
	ClosurePacketID string // AC11
	SHA256          string // AC11 manifest digest
}

// GuidedIntegrityDiscoveryRevision is one integrated discovery revision with
// its closure packet binding, predecessor linkage, and explicit
// current-vs-historical marker.
type GuidedIntegrityDiscoveryRevision struct {
	RevisionID      string // AC12
	RevisionNumber  int64  // AC12
	ClosurePacketID string // AC12 closure packet ID
	PacketSHA256    string // AC12 closure packet digest
	PredecessorID   string // AC12 predecessor revision ID
	Historical      bool   // AC25
}

// GuidedIntegrityReopenEvent links a reopened closure packet to its
// replacement integrated revision.
type GuidedIntegrityReopenEvent struct {
	ReopenEventID         string // AC12
	ReopenedPacketID      string // AC12 reopened closure packet
	ReplacementRevisionID string // AC12 replacement revision
}

// GuidedIntegrityAuthorityRevision is one authority revision with its layers.
type GuidedIntegrityAuthorityRevision struct {
	AuthorityRevisionID string                          // AC13
	RevisionNumber      int64                           // AC13
	Historical          bool                            // AC13/AC25 explicit current/historical
	Layers              []GuidedIntegrityAuthorityLayer // AC14
}

// GuidedIntegrityAuthorityLayer carries the layer kind, the exact stable
// artifact identity (the public discovery-artifact domain ID, never the row
// ID), the artifact digest, and the source closure identity.
type GuidedIntegrityAuthorityLayer struct {
	Kind            string // AC14
	ArtifactID      string // AC14 stable artifact identity
	SHA256          string // AC14 digest
	SourceClosureID string // AC14 source closure identity
}

// GuidedIntegrityPlanningCandidate is one planning candidate with its stable
// artifact identity, digest, size, explicit current-vs-historical basis,
// promotion linkage, and approval identities.
type GuidedIntegrityPlanningCandidate struct {
	CandidateID string   // AC15
	Family      string   // AC15
	ArtifactID  string   // AC15 stable artifact identity
	SHA256      string   // AC15 digest
	SizeBytes   int64    // AC15 size
	Historical  bool     // AC25 current-vs-historical
	Promoted    bool     // AC15 promotion linkage
	Approvals   []string // AC16 approval IDs
}

// GuidedIntegrityDeliverySection carries the delivery-owner identities: the
// Delivery Ticket frontier, current selection, execution package and its
// approval, the linked Run, the workflow audit, and audit remediation seeds.
type GuidedIntegrityDeliverySection struct {
	Frontier    []GuidedIntegrityTicket   // AC17
	Selection   *GuidedIntegritySelection // AC18
	Package     *GuidedIntegrityPackage     // AC19/AC20
	Run         *GuidedIntegrityRun         // AC21
	Audit       *GuidedIntegrityAudit       // AC22
	Remediation *GuidedIntegrityRemediation // AC23
}

type GuidedIntegrityTicket struct {
	TicketID       string // AC17
	RevisionNumber int64  // AC17
}
type GuidedIntegritySelection struct {
	SelectionID, State, TicketID string // AC18
	RevisionNumber               int64
}

type GuidedIntegrityPackage struct {
	PackageID  string // AC19
	SHA256     string // AC19
	ApprovalID string // AC20
}
type GuidedIntegrityRun struct {
	RunID      string // AC21
	PackageID  string // AC21 package basis
	RepoTarget string // AC21 current basis
	Branch     string // AC21 current basis
	BaseCommit string // AC21 current basis
}
type GuidedIntegrityAudit struct {
	AuditPacketID   string // AC22
	AuditDecisionID string // AC22
	AuditedCommit   string // AC22
}
type GuidedIntegrityRemediation struct {
	SeedIDs []string // AC23
}

// GuidedIntegrityPrototypeSection carries the prototype Run's authorization,
// proposal, and approval identities, its discovery basis binding, the exact
// cleanup obligation semantic keys, and the QA packets with their admission
// and evidence identities and digests.
type GuidedIntegrityPrototypeSection struct {
	RunID               string // AC24
	RunState            string // AC24
	ProposalID          string // AC24
	AuthorizationID     string // AC24
	ApprovalID          string // AC24
	DiscoveryRevisionID string // AC24 discovery basis binding
	Cleanup             []GuidedIntegrityPrototypeCleanup
	QAPackets           []GuidedIntegrityPrototypeQAPacket
}

type GuidedIntegrityPrototypeCleanup struct {
	CleanupObligationID string // AC24 exact semantic key
	Kind                string
	Status              string
}

type GuidedIntegrityPrototypeQAPacket struct {
	QAPacketID  string                               // AC24
	Status      string                               // AC24
	AdmissionID string                               // AC24 admission ID
	Evidence    []GuidedIntegrityPrototypeQAEvidence // AC24 evidence identities/digests
}

type GuidedIntegrityPrototypeQAEvidence struct {
	QaEvidenceID string // AC24
	SemanticRole string
	SHA256       string // AC24 digest
	SizeBytes    int64
	MediaType    string
}

// GuidedHandoff transfers the actual owner-composed operation surface for the
// bounded role operation instead of generic counts or routes. Transfer carries
// only public owner identities and semantic state resolved server-side.
type GuidedHandoff struct {
	Role, Summary, ResumeRoute string
	Context                    map[string]string
	Transfer                   *GuidedOperationTransfer
}

type GuidedOperationTransfer struct {
	Frontier        []GuidedFrontierEntry
	Members         []string // planning closure member semantic roles
	AuthorityLayers []string // planning authority layer kinds
	Ticket          *GuidedTicketTransfer
	Package         *GuidedPackageTransfer
	Run             *GuidedRunTransfer
	Audit           *GuidedAuditTransfer
	Remediation     *GuidedRemediationTransfer
	Prototype       *GuidedPrototypeTransfer
}

type GuidedTicketTransfer struct {
	TicketID       string
	RevisionNumber int64
	Readiness      []string
	// OperationID identifies the established planner operation that owns the
	// Delivery Ticket. The ticket's canonical JSON is deliberately not
	// substituted for a different artifact here.
	OperationID string
}
type GuidedPackageTransfer struct {
	PackageID string
	State     string
}
type GuidedRunTransfer struct {
	RunID, Status, RepoTarget, Branch, BaseCommit, PackageID string
}
type GuidedAuditTransfer struct {
	RunID, RunStatus, AuditState, AuditPacketID, AuditedCommit string
}
type GuidedRemediationTransfer struct {
	State   string
	SeedIDs []string
}
type GuidedPrototypeTransfer struct {
	RunID, RunState, ProcessOutcome string
	Cleanup                         []GuidedCleanupTransfer
	QAPackets                       []GuidedQAPacketTransfer
}
type GuidedCleanupTransfer struct {
	Kind, Status string
}
type GuidedQAPacketTransfer struct {
	PacketID string
	Status   string
	Evidence []string
}

type GuidedActionInput struct {
	WorkspaceID, Action string
	ExpectedVersion     int64
	Confirmation        bool
	Destination         DiscoveryDestination
	// ReopenDiscovery content: the operator-authored replacement integrated
	// revision. These are user content, not internal identities; the current
	// closure packet basis is resolved server-side and the replacement digest
	// is derived from the submitted markdown by the server. The client never
	// supplies a SHA-256 digest for a guided action.
	Cause        string
	Markdown     []byte
	Continuation string
}
type GuidedActionResult struct {
	Projection GuidedFeatureProjection
	Handoff    *GuidedHandoff
}

// ReadGuidedProjection composes all currently exposed Feature-owned state and
// the existing downstream store owners into one resumable semantic journey.
func (s *Service) ReadGuidedProjection(ctx context.Context, workspaceID string) (GuidedFeatureProjection, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	project, err := s.store.GetProjectByRowID(ctx, workspace.ProjectRowID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	assessment, err := s.AssessDiscoveryDestination(ctx, workspace.WorkspaceID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	currentness, err := s.Currentness(ctx, workspace.WorkspaceID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	authority, err := s.ReadAuthority(ctx, workspace.WorkspaceID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	completion, err := s.EvaluateCompletion(ctx, workspace.WorkspaceID)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	planning, err := s.guidedPlanning(ctx, workspace, currentness, authority)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	delivery, err := s.guidedDelivery(ctx, workspace)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	prototype, err := s.guidedPrototype(ctx, workspace)
	if err != nil {
		return GuidedFeatureProjection{}, err
	}
	projection := composeGuidedFeatureProjection(workspace, project, assessment, currentness, authority, completion, planning, delivery, prototype)
	projection.Diagnostics.Integrity = guidedIntegrity(ctx, s, workspace, assessment, authority, delivery, prototype)
	return projection, nil
}

func guidedIntegrity(ctx context.Context, s *Service, workspace workflowstore.FeatureWorkspace, assessment DiscoveryAssessment, authority []AuthorityRevisionDetail, delivery GuidedDeliverySection, prototype GuidedPrototypeSection) GuidedIntegritySection {
	result := GuidedIntegritySection{Delivery: GuidedIntegrityDeliverySection{}}
	diagnostic := func(domain, condition string) {
		result.Diagnostics = append(result.Diagnostics, GuidedIntegrityDiagnostic{Domain: domain, Condition: condition})
	}

	// Discovery (AC10-AC12): the current integrated revision and closure
	// packet, then the full revision history with its closure packet bindings,
	// predecessor linkage, and reopen replacement events.
	revisions, revisionErr := s.store.ListIntegratedDiscoveryRevisions(ctx, workspace.ID)
	packets, packetErr := s.store.ListDiscoveryClosurePackets(ctx, workspace.ID)
	reopenEvents, reopenErr := s.store.ListDiscoveryReopenEvents(ctx, workspace.ID)
	revisionIDByRow := make(map[int64]string, len(revisions))
	packetByRow := make(map[int64]workflowstore.DiscoveryClosurePacket, len(packets))
	packetByClosingRevision := make(map[int64]workflowstore.DiscoveryClosurePacket, len(packets))
	for _, revision := range revisions {
		revisionIDByRow[revision.ID] = revision.DiscoveryRevisionID
	}
	for _, packet := range packets {
		packetByRow[packet.ID] = packet
		packetByClosingRevision[packet.ClosingRevisionRowID] = packet
	}
	if revisionErr != nil {
		diagnostic("discovery", "unreadable")
	}
	if packetErr != nil {
		diagnostic("discovery", "unreadable")
	}
	if reopenErr != nil {
		diagnostic("discovery", "unreadable")
	}
	if assessment.Revision != nil {
		result.Discovery.CurrentRevisionID = assessment.Revision.DiscoveryRevisionID // AC10
	}
	if workspace.CurrentDiscoveryClosurePacketRowID.Valid {
		if packet, ok := packetByRow[workspace.CurrentDiscoveryClosurePacketRowID.Int64]; ok {
			result.Discovery.CurrentPacket = &GuidedIntegrityClosurePacket{ClosurePacketID: packet.ClosurePacketID, SHA256: packet.ManifestSha256} // AC11
		} else if packetErr == nil {
			diagnostic("discovery", "inconsistent")
		}
	}
	if revisionErr == nil {
		for _, revision := range revisions {
			entry := GuidedIntegrityDiscoveryRevision{RevisionID: revision.DiscoveryRevisionID, RevisionNumber: revision.RevisionNumber, Historical: true}
			if assessment.Revision != nil && revision.ID == assessment.Revision.ID {
				entry.Historical = false // AC25
			}
			if revision.PredecessorRevisionRowID.Valid {
				entry.PredecessorID = revisionIDByRow[revision.PredecessorRevisionRowID.Int64]
			}
			if packet, ok := packetByClosingRevision[revision.ID]; ok {
				entry.ClosurePacketID = packet.ClosurePacketID
				entry.PacketSHA256 = packet.ManifestSha256
			}
			result.Discovery.History = append(result.Discovery.History, entry)
		}
	}
	if reopenErr == nil && revisionErr == nil && packetErr == nil {
		for _, event := range reopenEvents {
			link := GuidedIntegrityReopenEvent{ReopenEventID: event.ReopenEventID, ReplacementRevisionID: revisionIDByRow[event.ReplacementRevisionRowID]}
			if link.ReplacementRevisionID == "" {
				diagnostic("discovery", "inconsistent")
			}
			if packet, ok := packetByRow[event.ClosurePacketRowID]; ok {
				link.ReopenedPacketID = packet.ClosurePacketID
			} else {
				diagnostic("discovery", "inconsistent")
			}
			result.Discovery.ReopenEvents = append(result.Discovery.ReopenEvents, link)
		}
	}

	// Authority (AC13-AC14): every revision with its explicit current or
	// historical marker, and every layer with its kind, exact stable artifact
	// identity (the public discovery-artifact domain ID, never the row ID),
	// artifact digest, and source closure identity.
	for _, detail := range authority {
		revision := GuidedIntegrityAuthorityRevision{
			AuthorityRevisionID: detail.Revision.AuthorityRevisionID,
			RevisionNumber:      detail.Revision.RevisionNumber,
			Historical:          detail.Historical, // AC13/AC25
		}
		for _, layer := range detail.Layers {
			entry := GuidedIntegrityAuthorityLayer{Kind: layer.LayerKind, SHA256: layer.ArtifactSha256}
			if layer.ArtifactRowID.Valid {
				entry.ArtifactID = discoveryArtifactIdentity(ctx, s, layer.ArtifactRowID.Int64, diagnostic, "authority")
			} else if layer.CandidateArtifactRowID.Valid {
				entry.ArtifactID = discoveryArtifactIdentity(ctx, s, layer.CandidateArtifactRowID.Int64, diagnostic, "authority")
			}
			if layer.SourceClosureRowID.Valid {
				entry.SourceClosureID = sourceClosureIdentity(ctx, s, layer.SourceClosureRowID.Int64, diagnostic, "authority")
			} else if detail.Revision.SourceClosureRowID.Valid {
				entry.SourceClosureID = sourceClosureIdentity(ctx, s, detail.Revision.SourceClosureRowID.Int64, diagnostic, "authority")
			}
			revision.Layers = append(revision.Layers, entry)
		}
		result.Authority = append(result.Authority, revision)
	}

	// Planning (AC15-AC16): every candidate with its stable artifact identity,
	// digest, size, explicit current-vs-historical basis, promotion linkage,
	// and approval identities. Currentness mirrors the guided planning rule:
	// a candidate promoted into the current authority on the current closure
	// packet remains current even when its row predates the authority.
	if candidates, err := s.store.ListPlanningCandidatesByWorkspace(ctx, workspace.ID); err == nil {
		for _, candidate := range candidates {
			promoted := candidatePromotedInCurrentAuthority(candidate, authority) // AC15
			entry := GuidedIntegrityPlanningCandidate{
				CandidateID: candidate.CandidateID,
				Family:      candidate.Family,
				SHA256:      candidate.ArtifactSha256,
				SizeBytes:   candidate.ArtifactSizeBytes,
				Historical:  !guidedPlanningCandidateCurrent(ctx, s, workspace, candidate, promoted), // AC25
				Promoted:    promoted,
			}
			if artifact, artifactErr := s.store.GetFeatureWorkspaceDiscoveryArtifactByRowID(ctx, candidate.ArtifactRowID); artifactErr == nil {
				entry.ArtifactID = artifact.DiscoveryArtifactID
			} else {
				diagnostic("planning", integrityReadCondition(artifactErr))
			}
			if approvals, approvalErr := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID); approvalErr == nil {
				for _, approval := range approvals {
					entry.Approvals = append(entry.Approvals, approval.ApprovalID) // AC16
				}
			} else {
				diagnostic("planning", integrityReadCondition(approvalErr))
			}
			result.Planning = append(result.Planning, entry)
		}
	} else {
		diagnostic("planning", integrityReadCondition(err))
	}

	// Delivery (AC17-AC23): owner-derived ticket, selection, package, Run,
	// audit, and remediation identities. Owners are optional inspection
	// sources; their absence leaves the identity surface empty rather than
	// failing the projection. The bound ticket owner is preferred so integrity
	// inspection observes the same instance as progression.
	for _, entry := range delivery.Frontier {
		result.Delivery.Frontier = append(result.Delivery.Frontier, GuidedIntegrityTicket{TicketID: entry.TicketID, RevisionNumber: entry.RevisionNumber}) // AC17
	}
	owner := s.guidedTickets
	if owner == nil {
		temporary, temporaryErr := apptickets.NewService(s.store)
		if temporaryErr != nil {
			diagnostic("delivery", "unavailable")
		} else {
			owner = temporary
		}
	}
	if owner != nil {
		if selection, selectionErr := owner.ReadWorkspaceSelection(ctx, workspace.WorkspaceID); selectionErr == nil && selection.SelectionID != "" {
			result.Delivery.Selection = &GuidedIntegritySelection{SelectionID: selection.SelectionID, State: selection.State, TicketID: selection.TicketID, RevisionNumber: selection.RevisionNumber} // AC18
		} else if selectionErr != nil {
			diagnostic("delivery", integrityReadCondition(selectionErr))
		}
	}
	if s.guidedPackages != nil {
		if packageState, packageErr := s.guidedPackages.ReadWorkspacePackageState(ctx, workspace.WorkspaceID); packageErr == nil && packageState.PackageID != "" {
			result.Delivery.Package = &GuidedIntegrityPackage{PackageID: packageState.PackageID, SHA256: packageState.PackageSHA256, ApprovalID: packageState.PackageApprovalID} // AC19/AC20
			if packageState.RunID != "" {
				result.Delivery.Run = &GuidedIntegrityRun{RunID: packageState.RunID, PackageID: packageState.PackageID, RepoTarget: packageState.RunRepoTarget, Branch: packageState.RunBranch, BaseCommit: packageState.RunBaseCommit} // AC21
			}
		} else if packageErr != nil {
			diagnostic("delivery.package", integrityReadCondition(packageErr))
		}
	} else {
		diagnostic("delivery.package", "unavailable")
	}
	if s.guidedAudit != nil {
		if result.Delivery.Run != nil {
			if auditState, auditErr := s.guidedAudit.ReadRunAuditState(ctx, result.Delivery.Run.RunID); auditErr == nil && auditState.AuditPacketID != "" {
				result.Delivery.Audit = &GuidedIntegrityAudit{AuditPacketID: auditState.AuditPacketID, AuditDecisionID: auditState.AuditDecisionID, AuditedCommit: auditState.AuditedCommit} // AC22
			} else if auditErr != nil {
				diagnostic("delivery.audit", integrityReadCondition(auditErr))
			}
		}
		if remediation, remediationErr := s.guidedAudit.ReadWorkspaceRemediationState(ctx, workspace.WorkspaceID); remediationErr == nil && len(remediation.SeedIDs) > 0 {
			result.Delivery.Remediation = &GuidedIntegrityRemediation{SeedIDs: append([]string(nil), remediation.SeedIDs...)} // AC23
		} else if remediationErr != nil {
			diagnostic("delivery.remediation", integrityReadCondition(remediationErr))
		}
	} else {
		diagnostic("delivery.audit", "unavailable")
		diagnostic("delivery.remediation", "unavailable")
	}

	// Prototype (AC24): the current prototype Run's authorization, proposal,
	// and approval identities, its discovery basis binding, the exact cleanup
	// obligation semantic keys, and the QA packets with their admission and
	// evidence identities and digests.
	if prototype.RunID != "" {
		result.Prototype = guidedIntegrityPrototype(ctx, s, workspace, prototype, diagnostic)
	}
	return result
}

// guidedIntegrityPrototype composes the prototype integrity surface from the
// Feature-owned prototype aggregate and wayfinder reads. It is best-effort:
// missing identity rows leave individual fields empty without failing the
// read-only inspection surface.
func guidedIntegrityPrototype(ctx context.Context, s *Service, workspace workflowstore.FeatureWorkspace, prototype GuidedPrototypeSection, diagnostic func(string, string)) *GuidedIntegrityPrototypeSection {
	result := &GuidedIntegrityPrototypeSection{RunID: prototype.RunID, RunState: prototype.RunState}
	aggregate, aggregateErr := s.store.ReadPrototypeExecution(ctx, workspace.WorkspaceID, prototype.RunID)
	if aggregateErr != nil {
		diagnostic("prototype", integrityReadCondition(aggregateErr))
		return result
	}
	result.ProposalID = aggregate.Proposal.ProposalID
	result.AuthorizationID = aggregate.Authorization.AuthorizationID
	if aggregate.Approval != nil {
		result.ApprovalID = aggregate.Approval.ApprovalID
	}
	if revisions, err := s.store.ListIntegratedDiscoveryRevisions(ctx, workspace.ID); err == nil {
		for _, revision := range revisions {
			if revision.ID == aggregate.Authorization.DiscoveryRevisionRowID {
				result.DiscoveryRevisionID = revision.DiscoveryRevisionID
				break
			}
		}
	} else {
		diagnostic("prototype", integrityReadCondition(err))
	}
	view, viewErr := s.ReadPrototypeEvidenceForWayfinder(ctx, workspace.WorkspaceID, prototype.RunID)
	if viewErr != nil {
		diagnostic("prototype", integrityReadCondition(viewErr))
		return result
	}
	for _, obligation := range view.Cleanup {
		result.Cleanup = append(result.Cleanup, GuidedIntegrityPrototypeCleanup{CleanupObligationID: obligation.CleanupObligationID, Kind: obligation.ObligationKind, Status: obligation.Status})
	}
	for _, packet := range view.QAPackets {
		entry := GuidedIntegrityPrototypeQAPacket{QAPacketID: packet.Packet.QAPacketID, Status: packet.Packet.Status}
		if packet.Admission != nil {
			entry.AdmissionID = packet.Admission.QAAdmissionID
		}
		for _, evidence := range packet.Evidence {
			entry.Evidence = append(entry.Evidence, GuidedIntegrityPrototypeQAEvidence{QaEvidenceID: evidence.QAEvidenceID, SemanticRole: evidence.SemanticRole, SHA256: evidence.SHA256, SizeBytes: evidence.SizeBytes, MediaType: evidence.MediaType})
		}
		result.QAPackets = append(result.QAPackets, entry)
	}
	return result
}

// discoveryArtifactIdentity resolves the public discovery-artifact domain ID
// for a row ID. Row IDs are never exposed on the integrity surface.
func discoveryArtifactIdentity(ctx context.Context, s *Service, rowID int64, diagnostic func(string, string), domain string) string {
	artifact, err := s.store.GetFeatureWorkspaceDiscoveryArtifactByRowID(ctx, rowID)
	if err != nil {
		diagnostic(domain, integrityReadCondition(err))
		return ""
	}
	return artifact.DiscoveryArtifactID
}

// sourceClosureIdentity resolves the public source-vault closure identity for
// a row ID.
func sourceClosureIdentity(ctx context.Context, s *Service, rowID int64, diagnostic func(string, string), domain string) string {
	closure, err := s.store.GetSourceVaultClosureByRowID(ctx, rowID)
	if err != nil {
		diagnostic(domain, integrityReadCondition(err))
		return ""
	}
	return closure.ClosureID
}

func integrityReadCondition(err error) string {
	if errors.Is(err, sql.ErrNoRows) {
		return "inconsistent"
	}
	return "unreadable"
}

func composeGuidedFeatureProjection(workspace workflowstore.FeatureWorkspace, project workflowstore.Project, assessment DiscoveryAssessment, currentness FeatureCurrentnessDecision, authority []AuthorityRevisionDetail, completion CompletionStatus, planning GuidedPlanningSection, delivery GuidedDeliverySection, prototype GuidedPrototypeSection) GuidedFeatureProjection {
	layers := make([]string, 0)
	currentRevisionNumber := 0
	for _, revision := range authority {
		if revision.Historical {
			continue
		}
		currentRevisionNumber = int(revision.Revision.RevisionNumber)
		for _, layer := range revision.Layers {
			layers = append(layers, layer.LayerKind)
		}
	}
	gates := make([]GuidedCompletionGate, 0, len(completion.Gates))
	for _, gate := range completion.Gates {
		gates = append(gates, GuidedCompletionGate{Name: gate.Name, Ready: gate.Ready})
	}
	decision := DecideGuidedFeatureAction(GuidedJourneyState{
		State: assessment.State, Destination: assessment.Destination, HasCurrentRevision: assessment.Revision != nil,
		AuthorityLayers: layers, Planning: planning, Delivery: delivery, Prototype: prototype,
		Continuation: assessment.Continuation, Blockers: assessment.Blockers,
		PendingIntegrations: assessment.PendingIntegrations, ActiveOperations: assessment.ActiveOperations,
		RouteMaterialOpen: assessment.RouteMaterialOpen, RequiredEvidence: assessment.RequiredEvidence,
	}, currentness, GuidedCompletion{Gates: gates, Recorded: completion.CurrentDecision != nil})
	primary := decision.AvailableActions[0]
	recovery := GuidedRecoverySection{State: "none", Category: decision.RecoveryCategory}
	if currentness.Readiness != FeatureCurrent {
		recovery.State = "required"
		recovery.Available = []string{currentness.RecoveryCategory}
	}
	decisionValue := ""
	if completion.CurrentDecision != nil {
		decisionValue = completion.CurrentDecision.Decision
	}
	return GuidedFeatureProjection{
		Workspace:   GuidedWorkspaceSection{WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug, State: workspace.State, Version: workspace.Version, CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt},
		Project:     GuidedProjectSection{ProjectID: project.ProjectID, Name: project.Name},
		Discovery:   GuidedDiscoverySection{State: string(assessment.State), Destination: string(assessment.Destination), Rationale: assessment.Rationale, Continuation: assessment.Continuation, Currentness: string(assessment.Currentness), Basis: currentness.Basis, ReopenState: guidedReopenState(assessment), HasCurrentRevision: assessment.Revision != nil, Blockers: append([]string(nil), assessment.Blockers...), RestorationActions: append([]string(nil), assessment.RestorationActions...), PendingIntegrations: append([]string(nil), assessment.PendingIntegrations...), ActiveOperations: append([]string(nil), assessment.ActiveOperations...), RouteMaterialOpen: append([]string(nil), assessment.RouteMaterialOpen...), RequiredEvidence: append([]string(nil), assessment.RequiredEvidence...)},
		Currentness: GuidedCurrentnessSection{Readiness: string(currentness.Readiness), Owner: currentness.StaleOwner, BlockedOperation: currentness.BlockedOperation, Effect: currentness.Effect, RecoveryCategory: currentness.RecoveryCategory},
		Authority:   GuidedAuthoritySection{CurrentRevisionNumber: currentRevisionNumber, Layers: append([]string(nil), layers...)},
		Planning:    planning, Delivery: delivery, Prototype: prototype,
		Completion:       GuidedCompletionSection{Gates: gates, Ready: GuidedCompletionReady(gates), Recorded: completion.CurrentDecision != nil, Decision: decisionValue},
		Recovery:         recovery,
		Diagnostics:      GuidedDiagnosticsSection{Stale: nonEmpty(currentness.StaleOwner, currentness.BlockedOperation, currentness.Effect), Historical: guidedHistoricalDiagnostics(currentness, assessment), Discovery: append([]string(nil), assessment.Blockers...), Delivery: append([]string(nil), delivery.Diagnostics...), Prototype: append([]string(nil), prototype.Diagnostics...)},
		AvailableActions: append([]GuidedFeatureActionAvailability(nil), decision.AvailableActions...),
		PrimaryAction:    primary,
	}
}

// guidedReopenState exposes the discovery reopen/reclosure basis: none for a
// workspace whose current revision was not produced by reopen, reopened while
// the replacement revision is open, and reclosed once the replacement revision
// is closed again.
func guidedReopenState(assessment DiscoveryAssessment) string {
	if assessment.Revision == nil || !assessment.Revision.PredecessorRevisionRowID.Valid {
		return "none"
	}
	if assessment.State == DiscoveryStateClosed {
		return "reclosed"
	}
	return "reopened"
}

func nonEmpty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func guidedHistoricalDiagnostics(currentness FeatureCurrentnessDecision, assessment DiscoveryAssessment) []string {
	if currentness.Readiness == FeatureStale || assessment.Currentness == DiscoveryHistorical {
		return []string{"historical_basis_requires_recovery"}
	}
	return []string{}
}

func candidatePromotedInCurrentAuthority(candidate workflowstore.PlanningCandidate, authority []AuthorityRevisionDetail) bool {
	for _, revision := range authority {
		if revision.Historical {
			continue
		}
		if candidatePromotedInCurrentAuthorityLayers(candidate, revision.Layers) {
			return true
		}
	}
	return false
}

func candidatePromotedInCurrentAuthorityLayers(candidate workflowstore.PlanningCandidate, layers []workflowstore.FeatureWorkspaceAuthorityLayer) bool {
	for _, layer := range layers {
		if layer.CandidateArtifactRowID.Valid && layer.CandidateArtifactRowID.Int64 == candidate.ArtifactRowID {
			return true
		}
	}
	return false
}

// guidedPlanningCandidateCurrent mirrors the guidedPlanning current-basis
// rule: a candidate is current when it is not historical, or when it was
// promoted into the current authority on the current closure packet (the row
// predates the authority but remains the current governing basis).
func guidedPlanningCandidateCurrent(ctx context.Context, s *Service, workspace workflowstore.FeatureWorkspace, candidate workflowstore.PlanningCandidate, promoted bool) bool {
	historical := s.planningCandidateHistorical(ctx, workspace, candidate)
	if !historical {
		return true
	}
	return promoted && workspace.CurrentDiscoveryClosurePacketRowID.Valid && candidate.DiscoveryClosurePacketRowID == workspace.CurrentDiscoveryClosurePacketRowID.Int64
}

func (s *Service) guidedPlanning(ctx context.Context, workspace workflowstore.FeatureWorkspace, currentness FeatureCurrentnessDecision, authority []AuthorityRevisionDetail) (GuidedPlanningSection, error) {
	candidates, err := s.store.ListPlanningCandidatesByWorkspace(ctx, workspace.ID)
	if err != nil {
		return GuidedPlanningSection{}, err
	}
	result := GuidedPlanningSection{Status: "not_started", CandidateState: "none", ReviewState: "none", ApprovalState: "none", PromotionState: "none"}
	requirements := GuidedPlanningFamilySection{}
	sharedDesign := GuidedPlanningFamilySection{}
	deliveryTicket := GuidedPlanningFamilySection{}
	// Candidates are immutable. Within one current basis, only the most recent
	// candidate in each family is progression authority; earlier replacements
	// remain inspectable history and must not make review or approval appear
	// available.
	seenCurrent := map[string]bool{}
	for index := len(candidates) - 1; index >= 0; index-- {
		candidate := candidates[index]
		promoted := candidatePromotedInCurrentAuthority(candidate, authority)
		historical := s.planningCandidateHistorical(ctx, workspace, candidate)
		if historical && !(promoted && workspace.CurrentDiscoveryClosurePacketRowID.Valid && candidate.DiscoveryClosurePacketRowID == workspace.CurrentDiscoveryClosurePacketRowID.Int64) {
			result.HistoricalCount++
			continue
		}
		var family *GuidedPlanningFamilySection
		switch candidate.Family {
		case CandidateFamilyRequirements:
			family = &requirements
		case CandidateFamilySharedDesign:
			family = &sharedDesign
		case CandidateFamilyDeliveryTicket:
			family = &deliveryTicket
		default:
			continue
		}
		if !promoted && seenCurrent[candidate.Family] {
			result.HistoricalCount++
			continue
		}
		if !promoted {
			seenCurrent[candidate.Family] = true
		}
		result.CandidateCount++
		approvals, approvalErr := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
		if approvalErr != nil {
			return GuidedPlanningSection{}, approvalErr
		}
		produced := false
		if candidate.Family == CandidateFamilyDeliveryTicket {
			links, linkErr := s.store.ListDeliveryTicketProductionLinksByCandidate(ctx, candidate.ID)
			if linkErr != nil {
				return GuidedPlanningSection{}, linkErr
			}
			produced = len(links) > 0
		}
		switch {
		case promoted:
			result.Promoted++
			family.Promoted++
		case produced:
			family.Produced++
		case len(approvals) > 0:
			result.AwaitingPromotion++
			family.AwaitingPromotion++
		case s.hasPlanningReviewContinuationFor(workspace.WorkspaceID, candidate.CandidateID):
			// A process-local ready review exists for this exact current
			// candidate. Review is complete but approval is still a distinct
			// explicit action; it is never fabricated from the review itself.
			result.AwaitingApproval++
			family.AwaitingApproval++
		default:
			result.AwaitingReview++
			family.AwaitingReview++
		}
		family.Count++
	}
	if result.CandidateCount > 0 {
		result.Status = "in_progress"
		switch {
		case result.Promoted == result.CandidateCount:
			result.Status = "promoted"
			result.CandidateState, result.ReviewState, result.ApprovalState, result.PromotionState = "promoted", "reviewed", "approved", "promoted"
		case result.AwaitingPromotion > 0:
			result.CandidateState, result.ReviewState, result.ApprovalState, result.PromotionState = "reviewed", "reviewed", "approved", "awaiting_promotion"
		case result.AwaitingApproval > 0:
			result.CandidateState, result.ReviewState, result.ApprovalState, result.PromotionState = "reviewed", "reviewed", "awaiting_approval", "none"
		case result.AwaitingReview > 0:
			result.CandidateState, result.ReviewState, result.ApprovalState, result.PromotionState = "admitted", "awaiting_review", "none", "none"
		}
	}
	if currentness.Readiness != FeatureCurrent && result.CandidateCount == 0 {
		result.Status = "blocked"
	}
	requirements.State = guidedPlanningFamilyState(requirements)
	sharedDesign.State = guidedPlanningFamilyState(sharedDesign)
	deliveryTicket.State = guidedPlanningFamilyState(deliveryTicket)
	result.Requirements = requirements
	result.SharedDesign = sharedDesign
	result.DeliveryTicket = deliveryTicket
	return result, nil
}

func guidedPlanningFamilyState(family GuidedPlanningFamilySection) string {
	switch {
	case family.Count > 0 && family.Produced == family.Count:
		return "produced"
	case family.Count > 0 && family.Promoted == family.Count:
		return "promoted"
	case family.AwaitingPromotion > 0:
		return "approved"
	case family.AwaitingApproval > 0:
		return "reviewed"
	case family.AwaitingReview > 0:
		return "admitted"
	default:
		return "none"
	}
}

// guidedDelivery composes the tickets, packages, and audits-owner semantic
// reads into the delivery projection. The Feature layer resolves identities
// from the owner reads and never derives lifecycle strings from rows itself.
// The tickets owner is the bound GuidedTicketOwner so the projection observes
// the same instance used for progression; a fresh owner is never constructed
// for progression reads.
func (s *Service) guidedDelivery(ctx context.Context, workspace workflowstore.FeatureWorkspace) (GuidedDeliverySection, error) {
	result := GuidedDeliverySection{SelectionState: "none", PackageState: "none", RunState: "none", AuditState: "none", RemediationState: "none"}
	if s.guidedTickets == nil {
		return result, ErrGuidedTicketOwnerUnavailable
	}
	tickets := s.guidedTickets
	frontier, err := tickets.ListFrontier(ctx, workspace.WorkspaceID)
	if err != nil {
		return result, err
	}
	for _, entry := range frontier.Entries {
		result.Frontier = append(result.Frontier, GuidedFrontierEntry{TicketID: entry.TicketID, RevisionNumber: entry.RevisionNumber, ExternalPriority: entry.ExternalPriority, RepoTarget: entry.RepoTarget, Branch: entry.Branch})
	}
	selection, err := tickets.ReadWorkspaceSelection(ctx, workspace.WorkspaceID)
	if err != nil {
		return result, err
	}
	result.SelectionState = selection.State
	if selection.State != "none" {
		if s.guidedPackages == nil {
			return result, ErrGuidedPackageOwnerUnavailable
		}
		packageState, err := s.guidedPackages.ReadWorkspacePackageState(ctx, workspace.WorkspaceID)
		if err != nil {
			return result, err
		}
		result.PackageState = packageState.State
		result.PackageID = packageState.PackageID
		result.RunState = packageState.RunStatus
		result.RunID = packageState.RunID
		if packageState.RunID != "" {
			if s.guidedAudit == nil {
				return result, ErrGuidedAuditOwnerUnavailable
			}
			auditState, err := s.guidedAudit.ReadRunAuditState(ctx, packageState.RunID)
			if err != nil {
				return result, err
			}
			result.AuditState = auditState.State
			result.AuditPacketID = auditState.AuditPacketID
			result.Diagnostics = append(result.Diagnostics, auditState.Diagnostics...)
		}
	}
	if s.guidedAudit == nil {
		return result, ErrGuidedAuditOwnerUnavailable
	}
	remediation, err := s.guidedAudit.ReadWorkspaceRemediationState(ctx, workspace.WorkspaceID)
	if err != nil {
		return result, err
	}
	result.RemediationState = remediation.State
	if remediation.State == "open" {
		result.Diagnostics = append(result.Diagnostics, "remediation_open")
	}
	if result.RunState == "needs_revision" {
		result.Diagnostics = append(result.Diagnostics, "run_needs_revision")
	}
	return result, nil
}

// guidedPrototype composes the Feature-owned prototype execution, cleanup, and
// QA semantic read for the current prototype Run. The Feature owner resolves
// the current Run and derives cleanup/QA states; this section only composes.
func (s *Service) guidedPrototype(ctx context.Context, workspace workflowstore.FeatureWorkspace) (GuidedPrototypeSection, error) {
	return s.ReadCurrentPrototypeState(ctx, workspace.WorkspaceID)
}

func guidedPrototypeDiagnostics(prototype GuidedPrototypeSection) []string {
	var diagnostics []string
	switch {
	case prototype.RunState == "proposed":
		diagnostics = append(diagnostics, "execution_ready_to_launch")
	case prototype.RunState == "cleanup_required" || prototype.CleanupState == "pending":
		diagnostics = append(diagnostics, "cleanup_pending")
	case prototype.RunState == "closed" && prototype.QAState == "prepared":
		diagnostics = append(diagnostics, "qa_evidence_pending")
	}
	if prototype.ProcessOutcome != "" {
		diagnostics = append(diagnostics, "process_outcome:"+prototype.ProcessOutcome)
	}
	return diagnostics
}

// ExecuteGuidedAction rechecks the current projection immediately before any
// mutation. Handoffs read the existing owner context and return a distinct
// resume projection; they do not create a second lifecycle or accept internal
// identities from the guided request.
func (s *Service) ExecuteGuidedAction(ctx context.Context, input GuidedActionInput) (GuidedActionResult, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || input.ExpectedVersion < 1 || strings.TrimSpace(input.Action) == "" {
		return GuidedActionResult{}, ErrGuidedActionBlocked
	}
	before, err := s.ReadGuidedProjection(ctx, input.WorkspaceID)
	if err != nil {
		return GuidedActionResult{}, err
	}
	if before.Workspace.Version != input.ExpectedVersion {
		return GuidedActionResult{}, ErrVersionConflict
	}
	requested := GuidedFeatureAction(input.Action)
	// The guided boundary executes exactly the presently enabled primary action
	// or the enabled secondary abandonment action. Abandonment is the only
	// accepted secondary: it re-reads the projection and version immediately
	// before dispatch and re-checks its own explicit confirmation, exactly like
	// any other confirmed mutation. Remaining advertised actions are display
	// surface for the operator; attempting them is rejected so the journey
	// cannot advance out of sequence.
	if requested == GuidedActionAbandonFeature {
		if !guidedActionAvailable(before.AvailableActions, GuidedActionAbandonFeature) {
			return GuidedActionResult{}, ErrGuidedActionBlocked
		}
		if !input.Confirmation {
			return GuidedActionResult{}, ErrFeatureCompletionConfirmation
		}
	} else {
		if before.PrimaryAction.Action != requested || !before.PrimaryAction.Enabled {
			return GuidedActionResult{}, ErrGuidedActionBlocked
		}
		if before.PrimaryAction.RequiresConfirmation && !input.Confirmation {
			return GuidedActionResult{}, ErrFeatureCompletionConfirmation
		}
	}
	switch requested {
	case GuidedActionContinueDiscovery:
		_, _, err = s.RecordDiscoveryDestinationAssessment(ctx, RecordDiscoveryDestinationAssessmentInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, CreatedIdentity: "guided-operator"})
	case GuidedActionCloseDiscovery:
		assessment, assessmentErr := s.AssessDiscoveryDestination(ctx, input.WorkspaceID)
		if assessmentErr != nil {
			return GuidedActionResult{}, assessmentErr
		}
		if assessment.Revision == nil {
			return GuidedActionResult{}, ErrDiscoveryNotStarted
		}
		destination := assessment.Destination
		if input.Destination != "" {
			destination = input.Destination
		}
		_, _, err = s.CloseFeatureDiscovery(ctx, CloseFeatureDiscoveryInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, ExpectedRevisionID: assessment.Revision.DiscoveryRevisionID, Destination: destination, CreatedIdentity: "guided-operator"})
	case GuidedActionCompleteFeature:
		_, err = s.Complete(ctx, CompletionInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, OperatorConfirmed: input.Confirmation})
	case GuidedActionAbandonFeature:
		_, err = s.Abandon(ctx, CompletionInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, OperatorConfirmed: input.Confirmation})
	case GuidedActionReopenDiscovery:
		_, _, err = s.guidedReopenDiscovery(ctx, input)
	case GuidedActionLegacyRecovery:
		_, _, err = s.AdoptFeatureDiscoveryLifecycle(ctx, AdoptFeatureDiscoveryLifecycleInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, OperatorIdentity: "guided-operator"})
	case GuidedActionAuthorRequirements, GuidedActionAuthorSharedDesign, GuidedActionContinueEstablishedRoute, GuidedActionReviewPlanningCandidate,
		GuidedActionAuthorDeliveryTicket,
		GuidedActionLaunchRun, GuidedActionContinueRun, GuidedActionRecoverRun, GuidedActionPrepareAudit, GuidedActionRecordAuditDecision,
		GuidedActionRemediate, GuidedActionPrototypeExecute, GuidedActionPrototypeCleanup, GuidedActionPrototypeQA:
		handoff, handoffErr := s.guidedHandoff(ctx, input.WorkspaceID, requested, before)
		if handoffErr != nil {
			return GuidedActionResult{}, handoffErr
		}
		before.Handoff = &handoff
		return GuidedActionResult{Projection: before, Handoff: &handoff}, nil
	case GuidedActionPromotePlanningCandidate:
		candidate, candidateErr := s.guidedCurrentPlanningCandidate(ctx, input.WorkspaceID, DiscoveryDestination(before.Discovery.Destination), true)
		if candidateErr != nil {
			return GuidedActionResult{}, candidateErr
		}
		if candidate.Family == CandidateFamilyDeliveryTicket {
			err = s.guidedProduceCurrentDeliveryTicketCandidate(ctx, input, candidate)
		} else {
			_, err = s.guidedPromoteCurrentCandidate(ctx, input, DiscoveryDestination(before.Discovery.Destination))
		}
	case GuidedActionApprovePlanningCandidate:
		// Explicit approval consumes the exact process-local continuation
		// stored by a ready review and revalidates the current candidate
		// server-side. The guided request carries no candidate identity or
		// digest; the operator confirmation is enforced by the action gate.
		_, err = s.ApproveCurrentPlanningCandidate(ctx, CandidateApprovalInput{
			WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion,
			OperatorConfirmationEvidence: guidedApprovalEvidence, CreatedIdentity: "guided-operator",
		})
	case GuidedActionSelectDeliveryTicket:
		err = s.guidedSelectFrontierTicket(ctx, input)
	case GuidedActionPreparePackage:
		if s.guidedPackages == nil {
			return GuidedActionResult{}, ErrGuidedPackageOwnerUnavailable
		}
		_, err = s.guidedPackages.PrepareCurrentSelection(ctx, guidedapp.PreparePackageInput{WorkspaceID: input.WorkspaceID})
	case GuidedActionApprovePackage:
		err = s.guidedApproveCurrentPackage(ctx, input)
	default:
		return GuidedActionResult{}, ErrGuidedActionBlocked
	}
	if err != nil {
		return GuidedActionResult{}, err
	}
	after, err := s.ReadGuidedProjection(ctx, input.WorkspaceID)
	return GuidedActionResult{Projection: after}, err
}

func (s *Service) guidedHandoff(ctx context.Context, workspaceID string, action GuidedFeatureAction, projection GuidedFeatureProjection) (GuidedHandoff, error) {
	handoff := GuidedHandoff{Role: string(action), ResumeRoute: "/feature-workspaces/" + workspaceID + "/guided", Context: map[string]string{"destination": projection.Discovery.Destination, "currentness": projection.Currentness.Readiness}}
	switch action {
	case GuidedActionAuthorRequirements, GuidedActionAuthorSharedDesign, GuidedActionContinueEstablishedRoute:
		// Compose the existing planner authoring and auditor review envelopes with
		// workspace context only. The guided handoff prepares the owner surface;
		// it never authors, reviews, approves, or promotes a candidate or ticket.
		operationID := guidedPlannerOperationForAction(action)
		if operationID != "" && !validGuidedOperation(operationID, registry.Role("planner")) {
			return GuidedHandoff{}, ErrGuidedActionBlocked
		}
		planner, err := s.ComposePlannerAuthoring(ctx, PlannerAuthoringInput{WorkspaceID: workspaceID})
		if err != nil {
			return GuidedHandoff{}, err
		}
		review, err := s.ReadAuditorReview(ctx, AuditorReviewInput{WorkspaceID: workspaceID})
		if err != nil {
			return GuidedHandoff{}, err
		}
		workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		candidateState, err := s.guidedFamilyCandidateState(ctx, workspace, DiscoveryDestination(projection.Discovery.Destination), planner.Authority)
		if err != nil {
			return GuidedHandoff{}, err
		}
		handoff.Context["owner"] = "planner_authoring_and_review"
		if operationID != "" {
			handoff.Context["operationId"] = operationID
		}
		handoff.Context["candidateState"] = candidateState
		handoff.Context["continuation"] = projection.Discovery.Continuation
		handoff.Transfer = &GuidedOperationTransfer{Members: closureMemberRoles(planner.Members), AuthorityLayers: authorityLayerKinds(review.Authority)}
		handoff.Summary = "Planner authoring and review are prepared through their existing owners. Author the current planning artifact, complete its read-only review and immediate explicit approval, then resume here to promote it."
	case GuidedActionAuthorDeliveryTicket:
		// Delivery Ticket authoring is the planner.delivery_ticket operation.
		// The produced Ticket subsequently enters the selection frontier; the
		// selected approved Delivery Ticket is the sole ticket semantic
		// authority, so no separate Ticket Design Brief stage exists.
		operationID := plannerDeliveryTicketOperation
		if !validGuidedOperation(operationID, registry.Role("planner")) {
			return GuidedHandoff{}, ErrGuidedActionBlocked
		}
		owner := "delivery_ticket_authoring"
		summary := "Enter the Delivery Ticket authoring operation (planner.delivery_ticket), then return here when the resulting Ticket is ready for selection."
		// Pre-approval review refreshes the ordinary delivery-ticket operation;
		// audit remediation has its own distinct lifecycle.
		handoff.Context["owner"] = owner
		handoff.Context["operationId"] = operationID
		handoff.Transfer = &GuidedOperationTransfer{Ticket: &GuidedTicketTransfer{OperationID: operationID}}
		handoff.Summary = summary
	case GuidedActionReviewPlanningCandidate:
		// Review is a read-only auditor preparation step. It composes the same
		// owner envelope the planner review path uses and never writes review or
		// approval state; approval is a separate explicit server-side action.
		candidate, err := s.guidedCurrentReviewablePlanningCandidate(ctx, workspaceID, DiscoveryDestination(projection.Discovery.Destination))
		if err != nil {
			return GuidedHandoff{}, err
		}
		review, err := s.ReadAuditorReview(ctx, AuditorReviewInput{WorkspaceID: workspaceID, CandidateID: candidate.CandidateID})
		if err != nil {
			return GuidedHandoff{}, err
		}
		workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		candidateState, err := s.guidedFamilyCandidateState(ctx, workspace, DiscoveryDestination(projection.Discovery.Destination), review.Authority)
		if err != nil {
			return GuidedHandoff{}, err
		}
		operationID := guidedAuditorReviewOperation(candidate.Family)
		if operationID != "" {
			if !validGuidedOperation(operationID, registry.Role("auditor")) {
				return GuidedHandoff{}, ErrGuidedActionBlocked
			}
			handoff.Context["owner"] = "auditor_review"
		} else {
			return GuidedHandoff{}, ErrGuidedActionBlocked
		}
		if operationID != "" {
			handoff.Context["operationId"] = operationID
		}
		handoff.Context["candidateState"] = candidateState
		handoff.Transfer = &GuidedOperationTransfer{Members: closureMemberRoles(review.Members), AuthorityLayers: authorityLayerKinds(review.Authority)}
		if operationID != "" {
			handoff.Summary = "The current Delivery Ticket candidate is prepared for the exact read-only auditor.delivery_ticket_review handoff. Record its bounded disposition through the planning candidate owner, then explicitly approve the exact reviewed candidate server-side before production."
		} else {
			handoff.Summary = "The auditor review surface is prepared through its existing owner envelope. A ready completion arms the distinct explicit approval action; approve the exact reviewed candidate server-side before resuming here to promote it."
		}
	case GuidedActionLaunchRun, GuidedActionContinueRun, GuidedActionRecoverRun:
		if s.guidedPackages == nil {
			return GuidedHandoff{}, ErrGuidedPackageOwnerUnavailable
		}
		packageState, err := s.guidedPackages.ReadWorkspacePackageState(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		if packageState.RunID == "" {
			return GuidedHandoff{}, ErrGuidedActionBlocked
		}
		transfer := &GuidedRunTransfer{RunID: packageState.RunID, Status: packageState.RunStatus, RepoTarget: packageState.RunRepoTarget, Branch: packageState.RunBranch, BaseCommit: packageState.RunBaseCommit, PackageID: packageState.PackageID}
		handoff.Context["owner"] = "package_run"
		handoff.Transfer = &GuidedOperationTransfer{Run: transfer}
		switch action {
		case GuidedActionLaunchRun:
			handoff.Summary = "The package Run is identified through its existing owner. Launch its initial execution through the Run owner, then return here for a fresh currentness check."
		case GuidedActionContinueRun:
			handoff.Summary = "The active package Run is identified through its existing owner. Continue or view its execution through the Run owner, then return here for a fresh currentness check."
		default:
			handoff.Summary = "The failed or cancelled package Run is identified through its existing owner. Use its supported recovery operation through the Run owner, then return here for a fresh currentness check."
		}
	case GuidedActionPrepareAudit, GuidedActionRecordAuditDecision:
		if projection.Delivery.RunID == "" {
			return GuidedHandoff{}, ErrGuidedActionBlocked
		}
		if s.guidedAudit == nil {
			return GuidedHandoff{}, ErrGuidedAuditOwnerUnavailable
		}
		auditState, err := s.guidedAudit.ReadRunAuditState(ctx, projection.Delivery.RunID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		transfer := &GuidedAuditTransfer{RunID: auditState.RunID, RunStatus: auditState.RunStatus, AuditState: auditState.State, AuditPacketID: auditState.AuditPacketID, AuditedCommit: auditState.AuditedCommit}
		handoff.Context["owner"] = "workflow_audit"
		handoff.Transfer = &GuidedOperationTransfer{Audit: transfer}
		handoff.Summary = "The workflow audit state is identified through the audit owner. Complete the audit preparation or decision through the audit owner, then return here."
	case GuidedActionRemediate:
		if s.guidedAudit == nil {
			return GuidedHandoff{}, ErrGuidedAuditOwnerUnavailable
		}
		remediation, err := s.guidedAudit.ReadWorkspaceRemediationState(ctx, workspaceID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		transfer := &GuidedRemediationTransfer{State: remediation.State, SeedIDs: append([]string(nil), remediation.SeedIDs...)}
		if !validGuidedOperation(plannerDeliveryTicketRemediationOperation, registry.Role("planner")) {
			return GuidedHandoff{}, ErrGuidedActionBlocked
		}
		handoff.Context["owner"] = "audit_remediation"
		handoff.Context["operationId"] = plannerDeliveryTicketRemediationOperation
		handoff.Transfer = &GuidedOperationTransfer{Remediation: transfer}
		handoff.Summary = "The audit remediation seed is identified through the audit owner. Publish the replacement Delivery Ticket revision bound to that seed, then return here to resume selection."
	case GuidedActionPrototypeExecute, GuidedActionPrototypeCleanup, GuidedActionPrototypeQA:
		transfer, err := s.guidedPrototypeTransfer(ctx, workspaceID, projection.Prototype.RunID)
		if err != nil {
			return GuidedHandoff{}, err
		}
		handoff.Context["owner"] = "prototype_execution"
		handoff.Transfer = &GuidedOperationTransfer{Prototype: transfer}
		handoff.Summary = "The prototype Run is identified through the prototype owner. Complete its execution, cleanup, or QA through the prototype owner, then return here."
	}
	return handoff, nil
}

func closureMemberRoles(members []workflowstore.DiscoveryClosurePacketMember) []string {
	roles := make([]string, 0, len(members))
	for _, member := range members {
		roles = append(roles, member.SemanticRole)
	}
	return roles
}

func authorityLayerKinds(layers []workflowstore.FeatureWorkspaceAuthorityLayer) []string {
	kinds := make([]string, 0, len(layers))
	for _, layer := range layers {
		kinds = append(kinds, layer.LayerKind)
	}
	return kinds
}

// validGuidedOperation keeps an exact handoff tied to the published source
// operation authority rather than treating a display string as an operation.
func validGuidedOperation(operationID string, role registry.Role) bool {
	operation, ok := registry.LookupPublishedOperation(registry.OperationID(operationID))
	return ok && operation.Role == role && operation.SourcePolicy != "" && operation.HistoricalAuthority != ""
}

func guidedPlannerOperationForAction(action GuidedFeatureAction) string {
	switch action {
	case GuidedActionAuthorRequirements:
		return plannerRequirementsOperation
	case GuidedActionAuthorSharedDesign:
		return plannerSharedDesignOperation
	default:
		return ""
	}
}

func guidedAuditorReviewOperation(family string) string {
	switch family {
	case CandidateFamilyRequirements:
		return auditorRequirementsReviewOperation
	case CandidateFamilySharedDesign:
		return auditorSharedDesignReviewOperation
	case CandidateFamilyDeliveryTicket:
		return auditorDeliveryTicketReviewOperation
	default:
		return ""
	}
}

// currentDiscoveryClosureContent resolves the current closure packet server-side
// and verifies it through the discovery owner read.
func (s *Service) currentDiscoveryClosureContent(ctx context.Context, workspaceID string) (DiscoveryPacketContent, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return DiscoveryPacketContent{}, err
	}
	if !workspace.CurrentDiscoveryClosurePacketRowID.Valid {
		return DiscoveryPacketContent{}, ErrDiscoveryNotClosed
	}
	packet, err := s.store.GetDiscoveryClosurePacketByRowID(ctx, workspace.CurrentDiscoveryClosurePacketRowID.Int64)
	if err != nil {
		return DiscoveryPacketContent{}, err
	}
	return s.ReadDiscoveryClosurePacket(ctx, workspaceID, packet.ClosurePacketID)
}

// guidedReopenDiscovery reopens the closed discovery through the Feature reopen
// owner. The current closure packet identity and expected basis are resolved
// server-side from the workspace; only the operator-authored replacement
// content and confirmation are accepted from the guided request.
func (s *Service) guidedReopenDiscovery(ctx context.Context, input GuidedActionInput) (workflowstore.IntegratedDiscoveryRevision, workflowstore.FeatureWorkspace, error) {
	content, err := s.currentDiscoveryClosureContent(ctx, strings.TrimSpace(input.WorkspaceID))
	if err != nil {
		return workflowstore.IntegratedDiscoveryRevision{}, workflowstore.FeatureWorkspace{}, err
	}
	return s.ReopenFeatureDiscovery(ctx, ReopenFeatureDiscoveryInput{
		WorkspaceID:       input.WorkspaceID,
		ExpectedVersion:   input.ExpectedVersion,
		ExpectedPacketID:  content.Packet.ClosurePacketID,
		OperatorConfirmed: input.Confirmation,
		Cause:             input.Cause,
		CreatedIdentity:   "guided-operator",
		SHA256:            digest(input.Markdown),
		Markdown:          input.Markdown,
		Destination:       input.Destination,
		Continuation:      input.Continuation,
	})
}

// guidedPrototypeTransfer composes the prototype owner view for the current
// prototype Run into the operation transfer surface.
func (s *Service) guidedPrototypeTransfer(ctx context.Context, workspaceID, runID string) (*GuidedPrototypeTransfer, error) {
	if runID == "" {
		return nil, ErrGuidedActionBlocked
	}
	view, err := s.ReadPrototypeEvidenceForWayfinder(ctx, workspaceID, runID)
	if err != nil {
		return nil, err
	}
	transfer := &GuidedPrototypeTransfer{RunID: view.RunID, RunState: view.RunState, ProcessOutcome: view.ProcessOutcome, Cleanup: make([]GuidedCleanupTransfer, 0, len(view.Cleanup)), QAPackets: make([]GuidedQAPacketTransfer, 0, len(view.QAPackets))}
	for _, obligation := range view.Cleanup {
		transfer.Cleanup = append(transfer.Cleanup, GuidedCleanupTransfer{Kind: obligation.ObligationKind, Status: obligation.Status})
	}
	for _, packet := range view.QAPackets {
		qapacket := GuidedQAPacketTransfer{PacketID: packet.Packet.QAPacketID, Status: packet.Packet.Status, Evidence: make([]string, 0, len(packet.Evidence))}
		for _, evidence := range packet.Evidence {
			qapacket.Evidence = append(qapacket.Evidence, evidence.SemanticRole)
		}
		transfer.QAPackets = append(transfer.QAPackets, qapacket)
	}
	return transfer, nil
}

// guidedSelectFrontierTicket resolves the current frontier head server-side and
// delegates the exact selection to the delivery owner. No ticket or revision
// identity is accepted from the client.
func (s *Service) guidedSelectFrontierTicket(ctx context.Context, input GuidedActionInput) error {
	if s.guidedTickets == nil {
		return ErrGuidedTicketOwnerUnavailable
	}
	owner := s.guidedTickets
	frontier, err := owner.ListFrontier(ctx, input.WorkspaceID)
	if err != nil {
		return err
	}
	if len(frontier.Entries) == 0 {
		return ErrGuidedActionBlocked
	}
	head := frontier.Entries[0]
	_, err = owner.Select(ctx, apptickets.SelectInput{
		WorkspaceID: input.WorkspaceID, TicketID: head.TicketID, RevisionRowID: head.RevisionRowID,
		Rationale: guidedApprovalEvidence,
	})
	return err
}

// guidedApproveCurrentPackage delegates the current prepared execution package
// approval to the package owner, which resolves the exact package identity and
// digest server-side. No package identity or digest is accepted from the
// client.
func (s *Service) guidedApproveCurrentPackage(ctx context.Context, input GuidedActionInput) error {
	if s.guidedPackages == nil {
		return ErrGuidedPackageOwnerUnavailable
	}
	return s.guidedPackages.ApproveCurrentPackage(ctx, guidedapp.ApprovePackageInput{WorkspaceID: input.WorkspaceID, Evidence: guidedApprovalEvidence})
}

// guidedApprovalEvidence is the server-side confirmation evidence recorded when
// the guided boundary approves a prepared package. The operator's confirmation
// of the guided approval action is enforced by the action gate.
const guidedApprovalEvidence = "guided-operator-approval"

// guidedActionAvailable reports whether the freshly read projection advertises
// the wanted action as an enabled availability. It backs the single accepted
// secondary action (abandonment); every other action stays primary-only.
func guidedActionAvailable(actions []GuidedFeatureActionAvailability, wanted GuidedFeatureAction) bool {
	for _, action := range actions {
		if action.Action == wanted && action.Enabled {
			return true
		}
	}
	return false
}

// guidedFamilyCandidateState resolves the semantic candidate state for the
// family currently in flight, matching the guided decision's family priority.
func (s *Service) guidedFamilyCandidateState(ctx context.Context, workspace workflowstore.FeatureWorkspace, destination DiscoveryDestination, layers []workflowstore.FeatureWorkspaceAuthorityLayer) (string, error) {
	candidates, err := s.store.ListPlanningCandidatesByWorkspace(ctx, workspace.ID)
	if err != nil {
		return "", err
	}
	for _, family := range guidedCandidateFamiliesForDestination(destination) {
		for index := len(candidates) - 1; index >= 0; index-- {
			candidate := candidates[index]
			promoted := candidatePromotedInCurrentAuthorityLayers(candidate, layers)
			historical := s.planningCandidateHistorical(ctx, workspace, candidate)
			if candidate.Family != family || (historical && !(promoted && workspace.CurrentDiscoveryClosurePacketRowID.Valid && candidate.DiscoveryClosurePacketRowID == workspace.CurrentDiscoveryClosurePacketRowID.Int64)) {
				continue
			}
			approvals, approvalErr := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
			if approvalErr != nil {
				return "", approvalErr
			}
			state := "admitted"
			if promoted {
				state = "promoted"
			} else if len(approvals) > 0 {
				state = "approved_awaiting_promotion"
			} else if s.hasPlanningReviewContinuationFor(workspace.WorkspaceID, candidate.CandidateID) {
				state = "reviewed_awaiting_approval"
			}
			return state, nil
		}
	}
	return "not_admitted", nil
}

// guidedCurrentPlanningCandidate resolves, without any client-supplied
// identity, the current-basis approved planning candidate awaiting promotion.
func (s *Service) guidedCurrentPlanningCandidate(ctx context.Context, workspaceID string, destination DiscoveryDestination, wantApproved bool) (workflowstore.PlanningCandidate, error) {
	return s.guidedCurrentPlanningCandidateWithState(ctx, workspaceID, destination, func(candidate workflowstore.PlanningCandidate, approvals []workflowstore.PlanningCandidateApproval) bool {
		return wantApproved && len(approvals) > 0
	})
}

// guidedCurrentReviewablePlanningCandidate resolves an admitted current
// candidate for the read-only auditor handoff. A candidate with any review
// disposition is no longer reviewable; in particular needs_revision must go
// to re-authoring rather than being treated as ready for approval.
func (s *Service) guidedCurrentReviewablePlanningCandidate(ctx context.Context, workspaceID string, destination DiscoveryDestination) (workflowstore.PlanningCandidate, error) {
	return s.guidedCurrentPlanningCandidateWithState(ctx, workspaceID, destination, func(candidate workflowstore.PlanningCandidate, approvals []workflowstore.PlanningCandidateApproval) bool {
		return len(approvals) == 0
	})
}

// guidedCurrentPlanningCandidateWithState selects only the newest candidate in
// each current family/basis. It does not derive frontier eligibility; produced
// candidates are excluded using the delivery owner's durable production link.
func (s *Service) guidedCurrentPlanningCandidateWithState(ctx context.Context, workspaceID string, destination DiscoveryDestination, matches func(workflowstore.PlanningCandidate, []workflowstore.PlanningCandidateApproval) bool) (workflowstore.PlanningCandidate, error) {
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return workflowstore.PlanningCandidate{}, err
	}
	authority, err := s.ReadAuthority(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return workflowstore.PlanningCandidate{}, err
	}
	candidates, err := s.store.ListPlanningCandidatesByWorkspace(ctx, workspace.ID)
	if err != nil {
		return workflowstore.PlanningCandidate{}, err
	}
	for _, family := range guidedCandidateFamiliesForDestination(destination) {
		for index := len(candidates) - 1; index >= 0; index-- {
			candidate := candidates[index]
			if candidate.Family != family || s.planningCandidateHistorical(ctx, workspace, candidate) || candidatePromotedInCurrentAuthority(candidate, authority) {
				continue
			}
			if candidate.Family == CandidateFamilyDeliveryTicket {
				links, linkErr := s.store.ListDeliveryTicketProductionLinksByCandidate(ctx, candidate.ID)
				if linkErr != nil {
					return workflowstore.PlanningCandidate{}, linkErr
				}
				if len(links) > 0 {
					break
				}
			}
			approvals, approvalErr := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
			if approvalErr != nil {
				return workflowstore.PlanningCandidate{}, approvalErr
			}
			if matches(candidate, approvals) {
				return candidate, nil
			}
			// The latest current candidate in a family is authoritative even if it
			// is at a different lifecycle stage. Never fall through to a stale
			// predecessor that happens to match this requested state.
			break
		}
	}
	return workflowstore.PlanningCandidate{}, ErrGuidedActionBlocked
}

// guidedPromoteCurrentCandidate promotes the current approved planning
// candidate server-side. The candidate and its approval are resolved from the
// workspace; the client never supplies their identities.
func (s *Service) guidedPromoteCurrentCandidate(ctx context.Context, input GuidedActionInput, destination DiscoveryDestination) (CandidatePromotionResult, error) {
	candidate, err := s.guidedCurrentPlanningCandidate(ctx, input.WorkspaceID, destination, true)
	if err != nil {
		return CandidatePromotionResult{}, err
	}
	approvals, err := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
	if err != nil || len(approvals) == 0 {
		return CandidatePromotionResult{}, ErrGuidedActionBlocked
	}
	return s.PromoteApprovedPlanningCandidate(ctx, CandidatePromotionInput{
		CandidateID: candidate.CandidateID, ApprovalID: approvals[0].ApprovalID,
		ExpectedVersion: input.ExpectedVersion, CreatedIdentity: "guided-operator",
	})
}

// guidedProduceCurrentDeliveryTicketCandidate delegates production to the
// ticket owner after resolving the current approved candidate and its approval
// server-side. The guided request supplies neither identity, digest, nor basis.
func (s *Service) guidedProduceCurrentDeliveryTicketCandidate(ctx context.Context, input GuidedActionInput, candidate workflowstore.PlanningCandidate) error {
	if candidate.Family != CandidateFamilyDeliveryTicket {
		return ErrGuidedActionBlocked
	}
	approvals, err := s.store.ListPlanningCandidateApprovalsByCandidate(ctx, candidate.ID)
	if err != nil || len(approvals) == 0 {
		return ErrGuidedActionBlocked
	}
	if s.guidedTickets == nil {
		return ErrGuidedTicketOwnerUnavailable
	}
	_, err = s.guidedTickets.PromoteApprovedDeliveryTicketCandidate(ctx, apptickets.CandidateProductionInput{
		CandidateID: candidate.CandidateID, ApprovalID: approvals[len(approvals)-1].ApprovalID,
		ExpectedVersion: input.ExpectedVersion, ExternalPriority: 0, CreatedIdentity: "guided-operator",
	})
	return err
}
