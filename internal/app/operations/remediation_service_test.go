package operations

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"relay/internal/operations/packet"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

type sequenceIDs struct {
	packet   atomic.Int64
	artifact atomic.Int64
}

func (s *sequenceIDs) PacketID() string   { return "opkt-test-" + decimal(s.packet.Add(1)) }
func (s *sequenceIDs) ArtifactID() string { return "artifact-test-" + decimal(s.artifact.Add(1)) }

type fixedClock struct {
	mu    sync.Mutex
	value time.Time
}

func (c *fixedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	value := c.value
	c.value = c.value.Add(time.Nanosecond)
	return value
}

func TestServiceCreateGetAndAuthorization(t *testing.T) {
	ctx := context.Background()
	store := openOperationServiceStore(t)
	service := newOperationService(t, store)
	document := operationDocument(t, "planner.plan")

	view, err := service.Create(ctx, CreateInput{Document: document})
	if err != nil {
		t.Fatal(err)
	}
	if view.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive || view.Summary.ReplacementPacket != nil {
		t.Fatalf("unexpected create summary: %+v", view.Summary)
	}
	if view.DocumentMediaType != packet.MediaType || view.DocumentSizeBytes != int64(len(view.DocumentBytes)) {
		t.Fatalf("unexpected document identity: %+v", view)
	}
	if err := packet.VerifyBytes(view.DocumentBytes, view.Summary.PacketSHA256, view.DocumentSizeBytes); err != nil {
		t.Fatal(err)
	}
	second, err := service.Get(ctx, view.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if string(second.DocumentBytes) != string(view.DocumentBytes) {
		t.Fatal("exact packet read changed persisted bytes")
	}
	second.DocumentBytes[0] = '['
	third, err := service.Get(ctx, view.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if third.DocumentBytes[0] != '{' {
		t.Fatal("packet view exposed mutable bytes")
	}

	authorized, err := service.AuthorizeMutation(ctx, MutationRequest{PacketID: view.Summary.PacketID, SurfaceContract: view.Summary.SurfaceContract, OperationID: view.Summary.OperationID, Action: "submit_plan"})
	if err != nil || !authorized.Allowed {
		t.Fatalf("active packet mutation authorization = %+v, %v", authorized, err)
	}
	_, err = service.AuthorizeMutation(ctx, MutationRequest{PacketID: view.Summary.PacketID, SurfaceContract: view.Summary.SurfaceContract, OperationID: view.Summary.OperationID, Action: "create_run"})
	if ErrorCode(err) != CodePacketActionNotAllowed {
		t.Fatalf("disallowed action error = %v", err)
	}

	packetRow, err := store.GetOperationPacketByPacketID(ctx, view.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.GetOperationPacketArtifact(ctx, packetRow.ID)
	if err != nil {
		t.Fatal(err)
	}
	read, err := service.AuthorizeRead(ctx, ReadRequest{PacketID: view.Summary.PacketID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID})
	if err != nil || read.OwnerIdentity != artifact.ArtifactID {
		t.Fatalf("packet document read authorization = %+v, %v", read, err)
	}
}

func TestServiceRefreshAndClosePreserveExactPriorReads(t *testing.T) {
	ctx := context.Background()
	store := openOperationServiceStore(t)
	service := newOperationService(t, store)
	created, err := service.Create(ctx, CreateInput{Document: operationDocument(t, "planner.requirements")})
	if err != nil {
		t.Fatal(err)
	}
	priorBytes := append([]byte(nil), created.DocumentBytes...)

	replacement, err := service.Refresh(ctx, RefreshInput{PriorPacketID: created.Summary.PacketID, Document: refreshDocument(t, "planner.requirements")})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive {
		t.Fatalf("replacement lifecycle = %q", replacement.Summary.LifecycleState)
	}
	prior, err := service.Get(ctx, created.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if prior.Summary.LifecycleState != workflowstore.OperationPacketLifecycleSuperseded || prior.Summary.ReplacementPacket == nil || prior.Summary.ReplacementPacket.PacketID != replacement.Summary.PacketID {
		t.Fatalf("unexpected superseded summary: %+v", prior.Summary)
	}
	if string(prior.DocumentBytes) != string(priorBytes) {
		t.Fatal("superseded read returned different or replacement bytes")
	}
	if _, err := service.AuthorizeMutation(ctx, MutationRequest{PacketID: created.Summary.PacketID}); ErrorCode(err) != CodePacketSuperseded {
		t.Fatalf("superseded mutation error = %v", err)
	}

	closed, err := service.Close(ctx, CloseInput{PacketID: replacement.Summary.PacketID})
	if err != nil {
		t.Fatal(err)
	}
	if closed.LifecycleState != workflowstore.OperationPacketLifecycleClosed || closed.ReplacementPacket != nil || closed.ClosedAt == nil {
		t.Fatalf("unexpected closed summary: %+v", closed)
	}
	if _, err := service.AuthorizeMutation(ctx, MutationRequest{PacketID: replacement.Summary.PacketID}); ErrorCode(err) != CodePacketClosed {
		t.Fatalf("closed mutation error = %v", err)
	}
	closedView, err := service.Get(ctx, replacement.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if closedView.Summary.PacketSHA256 != replacement.Summary.PacketSHA256 || string(closedView.DocumentBytes) != string(replacement.DocumentBytes) {
		t.Fatal("close changed immutable packet authority")
	}
}

func TestConcurrentRefreshHasOneWinner(t *testing.T) {
	ctx := context.Background()
	store := openOperationServiceStore(t)
	service := newOperationService(t, store)
	created, err := service.Create(ctx, CreateInput{Document: operationDocument(t, "planner.requirements")})
	if err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := service.Refresh(ctx, RefreshInput{PriorPacketID: created.Summary.PacketID, Document: refreshDocument(t, "planner.requirements")})
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	conflicts := 0
	for err := range results {
		switch ErrorCode(err) {
		case "":
			if err != nil {
				t.Fatal(err)
			}
			successes++
		case CodePacketRefreshConflict, CodePacketSuperseded:
			conflicts++
		default:
			t.Fatalf("unexpected refresh result: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("refresh results successes=%d conflicts=%d", successes, conflicts)
	}
	prior, err := service.Get(ctx, created.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if prior.Summary.ReplacementPacket == nil {
		t.Fatal("winning direct replacement is missing")
	}
}

func TestRetainedAuthorityAndArtifactMismatchFailBoundedly(t *testing.T) {
	ctx := context.Background()
	store := openOperationServiceStore(t)
	service := newOperationService(t, store)
	created, err := service.Create(ctx, CreateInput{Document: operationDocument(t, "planner.requirements")})
	if err != nil {
		t.Fatal(err)
	}
	packetRow, err := store.GetOperationPacketByPacketID(ctx, created.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.GetOperationPacketArtifact(ctx, packetRow.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.AuthorizeRead(ctx, ReadRequest{PacketID: created.Summary.PacketID, DependencyClass: workflowstore.OperationPacketDependencyInputArtifact, DependencyKey: "artifact-missing"})
	var coded *Error
	if !errors.As(err, &coded) || coded.Code != CodeRetainedAuthorityUnavailable || coded.MissingDependencyClass != workflowstore.OperationPacketDependencyInputArtifact {
		t.Fatalf("missing retained authority error = %#v", err)
	}

	path := filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath))
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Get(ctx, created.Summary.PacketID); ErrorCode(err) != CodePacketArtifactMismatch {
		t.Fatalf("tampered packet error = %v", err)
	}
}

func TestPacketOwnedRequiredAuthorityCannotBeBypassed(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name string
		call func(*Service, string) error
	}{
		{name: "refresh", call: func(service *Service, packetID string) error {
			_, err := service.Refresh(ctx, RefreshInput{PriorPacketID: packetID, Document: refreshDocument(t, "planner.requirements")})
			return err
		}},
		{name: "close", call: func(service *Service, packetID string) error {
			_, err := service.Close(ctx, CloseInput{PacketID: packetID})
			return err
		}},
		{name: "mutation_with_empty_caller_dependencies", call: func(service *Service, packetID string) error {
			_, err := service.AuthorizeMutation(ctx, MutationRequest{PacketID: packetID, SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements"})
			return err
		}},
		{name: "read", call: func(service *Service, packetID string) error {
			_, err := service.AuthorizeRead(ctx, ReadRequest{PacketID: packetID, DependencyClass: workflowstore.OperationPacketDependencyInputArtifact, DependencyKey: "artifact-unrelated"})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := openOperationServiceStore(t)
			service := newOperationService(t, store)
			created, err := service.Create(ctx, CreateInput{Document: operationDocument(t, "planner.requirements")})
			if err != nil {
				t.Fatal(err)
			}
			packetRow, artifact := packetRowAndArtifact(t, ctx, store, created.Summary.PacketID)
			if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
				_, err := tx.UpdateOperationPacketDependencyAvailability(ctx, workflowstore.UpdateOperationPacketDependencyAvailabilityParams{PacketRowID: packetRow.ID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: "artifact-other", Valid: true}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			err = test.call(service, created.Summary.PacketID)
			var coded *Error
			if !errors.As(err, &coded) || coded.Code != CodeRetainedAuthorityUnavailable || coded.MissingDependencyClass != workflowstore.OperationPacketDependencyPacketDocument {
				t.Fatalf("authority error = %#v", err)
			}
		})
	}
}

func TestRequiredDependencyOwnerAndArtifactIdentityFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("owner_mismatch", func(t *testing.T) {
		store := openOperationServiceStore(t)
		service := newOperationService(t, store)
		created, err := service.Create(ctx, CreateInput{Document: operationDocument(t, "planner.requirements")})
		if err != nil {
			t.Fatal(err)
		}
		packetRow, artifact := packetRowAndArtifact(t, ctx, store, created.Summary.PacketID)
		if err := store.WithTx(ctx, func(tx *workflowstore.Tx) error {
			_, err := tx.UpdateOperationPacketDependencyAvailability(ctx, workflowstore.UpdateOperationPacketDependencyAvailabilityParams{PacketRowID: packetRow.ID, DependencyClass: workflowstore.OperationPacketDependencyPacketDocument, DependencyKey: artifact.ArtifactID, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: "artifact-other", Valid: true}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Get(ctx, created.Summary.PacketID); ErrorCode(err) != CodeRetainedAuthorityUnavailable {
			t.Fatalf("owner mismatch error = %v", err)
		}
	})

	for _, test := range []struct {
		name string
		data []byte
	}{{name: "size_mismatch", data: []byte("tampered-size")}, {name: "digest_mismatch", data: []byte("{}")}} {
		t.Run(test.name, func(t *testing.T) {
			store := openOperationServiceStore(t)
			service := newOperationService(t, store)
			created, err := service.Create(ctx, CreateInput{Document: operationDocument(t, "planner.requirements")})
			if err != nil {
				t.Fatal(err)
			}
			_, artifact := packetRowAndArtifact(t, ctx, store, created.Summary.PacketID)
			path := filepath.Join(store.ArtifactStore().Root(), filepath.FromSlash(artifact.RelativePath))
			data := test.data
			if test.name == "digest_mismatch" {
				data = bytes.Repeat([]byte{'x'}, int(artifact.SizeBytes))
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := service.Get(ctx, created.Summary.PacketID); ErrorCode(err) != CodePacketArtifactMismatch {
				t.Fatalf("artifact mismatch error = %v", err)
			}
		})
	}
}

func TestConcurrentRefreshCloseAndRetainedReadsRemainConsistent(t *testing.T) {
	ctx := context.Background()
	store := openOperationServiceStore(t)
	service := newOperationService(t, store)
	created, err := service.Create(ctx, CreateInput{Document: operationDocument(t, "planner.requirements")})
	if err != nil {
		t.Fatal(err)
	}
	originalBytes := append([]byte(nil), created.DocumentBytes...)
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, err := service.Refresh(ctx, RefreshInput{PriorPacketID: created.Summary.PacketID, Document: refreshDocument(t, "planner.requirements")})
		results <- err
	}()
	go func() {
		<-start
		_, err := service.Close(ctx, CloseInput{PacketID: created.Summary.PacketID})
		results <- err
	}()
	close(start)
	first, second := <-results, <-results
	if first != nil && second != nil {
		t.Fatalf("refresh and close both failed: %v / %v", first, second)
	}
	view, err := service.Get(ctx, created.Summary.PacketID)
	if err != nil {
		t.Fatal(err)
	}
	if string(view.DocumentBytes) != string(originalBytes) {
		t.Fatal("terminal transition changed prior packet bytes")
	}
	if view.Summary.LifecycleState != workflowstore.OperationPacketLifecycleSuperseded && view.Summary.LifecycleState != workflowstore.OperationPacketLifecycleClosed {
		t.Fatalf("invalid terminal state: %+v", view.Summary)
	}

	const readers = 32
	var wait sync.WaitGroup
	errorsOut := make(chan error, readers)
	for range readers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			read, err := service.Get(ctx, created.Summary.PacketID)
			if err != nil {
				errorsOut <- err
				return
			}
			if string(read.DocumentBytes) != string(originalBytes) {
				errorsOut <- errors.New("retained read changed bytes")
			}
		}()
	}
	wait.Wait()
	close(errorsOut)
	for err := range errorsOut {
		t.Fatal(err)
	}
}

func TestIndependentPacketLineagesCoexist(t *testing.T) {
	ctx := context.Background()
	store := openOperationServiceStore(t)
	service := newOperationService(t, store)
	first, err := service.Create(ctx, CreateInput{Document: operationDocument(t, "planner.requirements")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(ctx, CreateInput{Document: operationDocument(t, "planner.requirements")})
	if err != nil {
		t.Fatal(err)
	}
	if first.Summary.PacketID == second.Summary.PacketID || first.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive || second.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive {
		t.Fatalf("independent lineages = %+v / %+v", first.Summary, second.Summary)
	}
}

func packetRowAndArtifact(t *testing.T, ctx context.Context, store *workflowstore.Store, packetID string) (workflowstore.OperationPacket, workflowstore.OperationPacketArtifact) {
	t.Helper()
	packetRow, err := store.GetOperationPacketByPacketID(ctx, packetID)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.GetOperationPacketArtifact(ctx, packetRow.ID)
	if err != nil {
		t.Fatal(err)
	}
	return packetRow, artifact
}

func TestServiceErrorsDoNotEchoCallerValues(t *testing.T) {
	service, err := NewService(nil)
	if err == nil || service != nil {
		t.Fatal("nil store was accepted")
	}
	store := openOperationServiceStore(t)
	service = newOperationService(t, store)
	secret := strings.Repeat("caller-secret-", 128)
	_, err = service.Get(context.Background(), secret)
	if strings.Contains(err.Error(), secret) {
		t.Fatal("bounded error echoed caller-controlled packet ID")
	}
}

func openOperationServiceStore(t *testing.T) *workflowstore.Store {
	t.Helper()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func newOperationService(t *testing.T, store *workflowstore.Store) *Service {
	t.Helper()
	service, err := NewServiceWithDependencies(store, &sequenceIDs{}, &fixedClock{value: time.Date(2026, 7, 15, 16, 4, 5, 123456789, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func operationDocument(t *testing.T, operationID registry.OperationID) packet.Document {
	t.Helper()
	operation, ok := registry.Lookup(operationID)
	if !ok {
		t.Fatalf("operation %q is missing", operationID)
	}
	manifestSHA, ok := registry.SurfaceManifestSHA256(operation.SurfaceContract)
	if !ok {
		t.Fatalf("surface manifest %q is missing", operation.SurfaceContract)
	}
	document := packet.Document{SchemaVersion: packet.SchemaVersion, Role: operation.Role, OperationID: operation.OperationID, SurfaceContract: operation.SurfaceContract, SurfaceManifestSHA256: manifestSHA, Output: packet.OutputContract{OutputKind: operation.OutputKind, OutputPersistence: operation.OutputPersistence}, Project: packet.ProjectBinding{ProjectID: "project-test"}, Repositories: []packet.RepositoryBinding{{RepositoryKey: "relay", RepositoryTarget: "relay", BindingOrder: 1, RevisionSource: packet.RevisionSourceExplicitCommit, RepositoryTargetConfigurationVersion: 1, CommitOID: strings.Repeat("1", 40), TreeOID: strings.Repeat("2", 40)}}, RelaySpecs: packet.GovernanceBinding{RepositoryKey: "relay-specs", RepositoryTarget: "relay-specs", Reserved: true, RevisionSource: packet.RevisionSourceExplicitCommit, RepositoryTargetConfigurationVersion: 1, CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40)}, ManifestDomain: packet.ManifestDomainBinding{ManifestPath: operationPathIdentity("planner-source-manifest.json"), ManifestBlobOID: strings.Repeat("c", 40), ManifestSHA256: strings.Repeat("d", 64), Domain: operation.ManifestDomain, Members: []packet.ManifestMember{{MemberOrder: 1, Path: operationPathIdentity("contracts/cross-cutting.md"), BlobOID: strings.Repeat("e", 40), ByteSize: 1, SHA256: strings.Repeat("f", 64)}}}, SourcePolicy: operation.SourcePolicy, HistoricalAuthority: operation.HistoricalAuthority, AllowedActions: append([]registry.AllowedAction(nil), operation.AllowedNonSourceActions...), ReadinessState: packet.ReadinessReady}
	for index, purpose := range operation.ComparisonAnchorPurposes {
		document.Repositories[0].Anchors = append(document.Repositories[0].Anchors, packet.Anchor{AnchorName: "anchor-" + decimal(int64(index+1)), Purpose: purpose, CommitOID: strings.Repeat("3", 40), TreeOID: strings.Repeat("4", 40)})
	}
	for _, kind := range operation.WorkflowReferenceKinds {
		document.WorkflowReferences = append(document.WorkflowReferences, operationWorkflowReference(kind))
	}
	fileIndex := int64(0)
	for _, slot := range operation.RequiredInputs {
		input := operationInput(slot, fileIndex, false)
		if input.SourceKind == packet.InputSourceUploadedFile {
			fileIndex++
		}
		document.Inputs = append(document.Inputs, input)
		document.Attestations = append(document.Attestations, operationAttestation(slot, input))
		if input.SourceKind != packet.InputSourceCommittedSource {
			document.Attestations = append(document.Attestations, operationSensitiveClearance(input))
		}
	}
	for _, slot := range operation.DerivedInputs {
		document.Inputs = append(document.Inputs, operationInput(slot, fileIndex, true))
	}
	return document
}

func operationWorkflowReference(kind registry.WorkflowReferenceKind) packet.WorkflowReference {
	switch kind {
	case "plan":
		return packet.WorkflowReference{Kind: kind, PlanID: "plan-1", CanonicalArtifactID: "artifact-plan", CanonicalArtifactSHA256: strings.Repeat("1", 64)}
	case "pass":
		return packet.WorkflowReference{Kind: kind, PlanID: "plan-1", PassID: "pass-1", PassNumber: 1}
	case "run":
		return packet.WorkflowReference{Kind: kind, RunID: "run-1", ExecutionSpecArtifactID: "artifact-spec", ExecutionSpecSHA256: strings.Repeat("2", 64)}
	case "audit_packet":
		return packet.WorkflowReference{Kind: kind, RunID: "run-1", AuditPacketID: "packet-1", AuditPacketSHA256: strings.Repeat("3", 64)}
	case "audit_decision":
		return packet.WorkflowReference{Kind: kind, RunID: "run-1", AuditDecisionID: "audit-1", Decision: "needs_revision", RecordedAt: "2026-07-15T16:04:05.123456789Z"}
	default:
		return packet.WorkflowReference{Kind: kind}
	}
}

func refreshDocument(t *testing.T, operationID registry.OperationID) packet.Document {
	document := operationDocument(t, operationID)
	operation, ok := registry.Lookup(operationID)
	if !ok {
		t.Fatalf("operation %q is missing", operationID)
	}
	var reviewedSHA string
	for _, slot := range operation.ConditionalRefreshInputs {
		input := operationInput(slot, 0, false)
		input.SourceKind = packet.InputSourceRelayArtifact
		input.Source = packet.InputSource{Kind: packet.InputSourceRelayArtifact, ArtifactID: "artifact-" + slot.InputName}
		if slot.InputName == "auditor_review_result" {
			input.SHA256 = strings.Repeat("9", 64)
		} else {
			reviewedSHA = input.SHA256
		}
		document.Inputs = append(document.Inputs, input)
		attestation := operationAttestation(slot, input)
		if slot.InputName == "auditor_review_result" {
			attestation.ReviewResult = "needs_revision"
			attestation.ReviewedCandidateSHA256 = reviewedSHA
			attestation.Complete = true
		}
		document.Attestations = append(document.Attestations, attestation, operationSensitiveClearance(input))
	}
	return document
}

func operationInput(slot registry.InputSlotDefinition, fileIndex int64, derived bool) packet.InputBinding {
	sourceKind := packet.InputSourceRelayArtifact
	if !derived && len(slot.AllowedSourceKinds) > 0 {
		sourceKind = slot.AllowedSourceKinds[0]
	}
	source := packet.InputSource{Kind: sourceKind}
	switch sourceKind {
	case packet.InputSourceUploadedFile:
		source.FileIndex = fileIndex
		source.ArtifactID = "artifact-upload"
	case packet.InputSourceRelayArtifact, packet.InputSourceInlineText:
		source.ArtifactID = "artifact-" + slot.InputName
	case packet.InputSourceWorkflowRecord:
		source.WorkflowReference = operationWorkflowReference("plan")
		source.SnapshotArtifactID = "artifact-snapshot"
		source.SnapshotSHA256 = strings.Repeat("4", 64)
	case packet.InputSourceCommittedSource:
		source.RepositoryBindingID = "binding-relay"
		source.CommitOID = strings.Repeat("5", 40)
		source.TreeOID = strings.Repeat("6", 40)
		source.Path = operationPathIdentity("internal/example.go")
		source.BlobOID = strings.Repeat("7", 40)
	}
	return packet.InputBinding{InputName: slot.InputName, InputRole: slot.InputRole, SourceKind: sourceKind, DisplayName: slot.InputName, MediaType: "application/octet-stream", SHA256: strings.Repeat("8", 64), SizeBytes: 8, AttestationKind: slot.AttestationKind, Source: source}
}

func operationAttestation(slot registry.InputSlotDefinition, input packet.InputBinding) packet.Attestation {
	value := packet.Attestation{Kind: slot.AttestationKind, InputName: slot.InputName}
	switch slot.AttestationKind {
	case "confirmed_intent":
		value.SubjectSHA256 = input.SHA256
		value.Confirmed = true
	case "approved_artifact":
		value.SubjectSHA256 = input.SHA256
		value.Approved = true
	case "candidate_for_review":
		value.SubjectSHA256 = input.SHA256
		value.CompleteTransfer = true
	case "execution_mode_selection":
		value.SelectedMode = "plan"
	case "complete_review_result":
		value.SubjectSHA256 = input.SHA256
		value.ReviewedCandidateSHA256 = strings.Repeat("d", 64)
		value.ReviewResult = "ready_for_approval"
		value.Complete = true
	case "completed_dependency_outcomes", "exact_evidence":
		value.SubjectSHA256 = input.SHA256
		value.Complete = true
	case "operator_confirmation", "separate_session_authorship":
		value.Confirmed = true
	}
	return value
}

func operationSensitiveClearance(input packet.InputBinding) packet.Attestation {
	return packet.Attestation{Kind: "sensitive_data_clearance", InputName: input.InputName, Clearance: &packet.SensitiveDataClearance{PolicyVersion: "relay.canonical-artifact-sensitive-data.v1", SubjectSHA256: input.SHA256, Confirmed: true}}
}

func operationPathIdentity(value string) packet.PathIdentity {
	bytes := []byte(value)
	digest := sha256.New()
	_, _ = digest.Write([]byte("relay.git-path.v1"))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write(bytes)
	return packet.PathIdentity{PathID: hex.EncodeToString(digest.Sum(nil)), ByteLength: int64(len(bytes)), PathBytesBase64: base64.StdEncoding.EncodeToString(bytes)}
}

func decimal(value int64) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[index:])
}
