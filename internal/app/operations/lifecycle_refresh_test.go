package operations

import (
	"sync"
	"testing"

	"relay/internal/app/idempotency"
	"relay/internal/mcp/fileacquisition"
	"relay/internal/mcp/semanticidentity"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

func createLifecycleRequirementsPacket(t *testing.T, fixture lifecycleFixture, mutationID string) CreateLifecycleResult {
	t.Helper()
	text := "Author exact requirements"
	sha := lifecycleSHA([]byte(text))
	clearance := registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: sha, Confirmed: true}
	identity := semanticidentity.CreateOperationPacket{
		SurfaceContract: "planner-authoring.v1", OperationID: "planner.requirements", ProjectID: fixture.projectID,
		Inputs: []semanticidentity.InputBinding{{InputName: "confirmed_intent", SourceKind: "inline_text", DisplayName: "intent.txt", MediaType: "text/plain", ExpectedSHA256: sha, Source: semanticidentity.InputBindingSource{Text: text}}},
		Attestations: []semanticidentity.AttestationRequest{
			{Kind: "confirmed_intent", InputName: "confirmed_intent", SubjectSHA256: sha, Confirmed: true},
			{Kind: "sensitive_data_clearance", InputName: "confirmed_intent", Clearance: &clearance},
		},
	}
	result, err := fixture.service.Create(fixture.ctx, CreateLifecycleInput{MutationID: mutationID, Identity: identity})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func lifecycleRefreshRequest(fixture lifecycleFixture, priorPacketID, mutationID string) RefreshLifecycleInput {
	intent := "Author exact requirements"
	intentSHA := lifecycleSHA([]byte(intent))
	candidate := []byte("replacement requirements")
	candidateSHA := lifecycleSHA(candidate)
	review := "needs revision: add exact proof"
	reviewSHA := lifecycleSHA([]byte(review))
	fixture.fetcher.mu.Lock()
	fixture.fetcher.files["candidate"] = candidate
	fixture.fetcher.mu.Unlock()
	index := int64(0)
	clearance := func(sha string) *registry.SensitiveDataClearance {
		return &registry.SensitiveDataClearance{PolicyVersion: registry.SensitiveDataClearancePolicyVersion, SubjectSHA256: sha, Confirmed: true}
	}
	identity := semanticidentity.RefreshOperationPacket{
		SurfaceContract: "planner-authoring.v1", ExpectedPacketID: priorPacketID,
		InputFileCount: 1, DeclaredFiles: []semanticidentity.DeclaredFile{{FileIndex: 0, ExpectedSHA256: candidateSHA}},
		Inputs: []semanticidentity.InputBinding{
			{InputName: "confirmed_intent", SourceKind: "inline_text", DisplayName: "intent.txt", MediaType: "text/plain", ExpectedSHA256: intentSHA, Source: semanticidentity.InputBindingSource{Text: intent}},
			{InputName: "reviewed_candidate", SourceKind: "uploaded_file", DisplayName: "candidate.md", MediaType: "text/markdown", ExpectedSHA256: candidateSHA, Source: semanticidentity.InputBindingSource{FileIndex: &index}},
			{InputName: "auditor_review_result", SourceKind: "inline_text", DisplayName: "review.md", MediaType: "text/markdown", ExpectedSHA256: reviewSHA, Source: semanticidentity.InputBindingSource{Text: review}},
		},
		Attestations: []semanticidentity.AttestationRequest{
			{Kind: "confirmed_intent", InputName: "confirmed_intent", SubjectSHA256: intentSHA, Confirmed: true},
			{Kind: "sensitive_data_clearance", InputName: "confirmed_intent", Clearance: clearance(intentSHA)},
			{Kind: "candidate_for_review", InputName: "reviewed_candidate", SubjectSHA256: candidateSHA, CompleteTransfer: true},
			{Kind: "sensitive_data_clearance", InputName: "reviewed_candidate", Clearance: clearance(candidateSHA)},
			{Kind: "complete_review_result", InputName: "auditor_review_result", SubjectSHA256: reviewSHA, ReviewedCandidateSHA256: candidateSHA, ReviewResult: "needs_revision", Complete: true},
			{Kind: "sensitive_data_clearance", InputName: "auditor_review_result", Clearance: clearance(reviewSHA)},
		},
	}
	return RefreshLifecycleInput{MutationID: mutationID, PriorPacketID: priorPacketID, Identity: identity, Files: []fileacquisition.FileParameter{{FileID: "candidate", FileName: "candidate.md", MIMEType: "text/markdown"}}}
}

func TestLifecycleRefreshReplayPrecedesActivePriorAndFileAcquisition(t *testing.T) {
	fixture := openLifecycleFixture(t)
	prior := createLifecycleRequirementsPacket(t, fixture, "create-prior")
	request := lifecycleRefreshRequest(fixture, prior.Packet.Summary.PacketID, "refresh-prior")
	first, err := fixture.service.Refresh(fixture.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Replay || first.Prior.LifecycleState != workflowstore.OperationPacketLifecycleSuperseded || first.Packet.Summary.LifecycleState != workflowstore.OperationPacketLifecycleActive {
		t.Fatalf("first refresh = %#v", first)
	}
	calls := fixture.fetcher.callCount()
	fixture.fetcher.setFail(true)
	replay, err := fixture.service.Refresh(fixture.ctx, request)
	if err != nil || !replay.Replay || replay.Packet.Summary.PacketID != first.Packet.Summary.PacketID || replay.Mutation.ResultSHA256 != first.Mutation.ResultSHA256 || fixture.fetcher.callCount() != calls {
		t.Fatalf("refresh replay = %#v calls=%d err=%v", replay, fixture.fetcher.callCount(), err)
	}

	conflict := request
	conflict.Identity = request.Identity
	conflict.Identity.Attestations = append([]semanticidentity.AttestationRequest(nil), request.Identity.Attestations...)
	conflict.Identity.Attestations[0].SubjectSHA256 = lifecycleSHA([]byte("other intent"))
	if _, err := fixture.service.Refresh(fixture.ctx, conflict); !idempotency.HasCode(err, idempotency.ErrorMutationConflict) || fixture.fetcher.callCount() != calls {
		t.Fatalf("semantic conflict = %v calls=%d", err, fixture.fetcher.callCount())
	}

	fresh := request
	fresh.MutationID = "refresh-after-superseded"
	if _, err := fixture.service.Refresh(fixture.ctx, fresh); ErrorCode(err) != CodePacketSuperseded || fixture.fetcher.callCount() != calls {
		t.Fatalf("fresh refresh after supersession = %v code=%q calls=%d", err, ErrorCode(err), fixture.fetcher.callCount())
	}
}

func TestLifecycleConcurrentEqualRefreshesReturnOneWinner(t *testing.T) {
	fixture := openLifecycleFixture(t)
	prior := createLifecycleRequirementsPacket(t, fixture, "create-concurrent-prior")
	request := lifecycleRefreshRequest(fixture, prior.Packet.Summary.PacketID, "refresh-concurrent")
	type outcome struct {
		result RefreshLifecycleResult
		err    error
	}
	start := make(chan struct{})
	out := make(chan outcome, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := fixture.service.Refresh(fixture.ctx, request)
			out <- outcome{result: result, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	values := make([]RefreshLifecycleResult, 0, 2)
	for value := range out {
		if value.err != nil {
			t.Fatal(value.err)
		}
		values = append(values, value.result)
	}
	if len(values) != 2 || values[0].Packet.Summary.PacketID != values[1].Packet.Summary.PacketID || values[0].Mutation.ResultSHA256 != values[1].Mutation.ResultSHA256 || (!values[0].Replay && !values[1].Replay) {
		t.Fatalf("concurrent outcomes = %#v", values)
	}
}
