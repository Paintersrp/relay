package auditor

import (
	"testing"

	"relay/internal/artifacts"
)

func TestCollectPacketMetadataUsesCanonicalValidationContract(t *testing.T) {
	setupTestArtifactDir(t)
	const runID = int64(812)
	packet := []byte(`{
  "execution_payload": {
    "goal": "test", "scope": "test", "non_goals": [], "file_targets": [],
    "validation_contract": {"commands": [{"id":"V-contract","command":"go test ./internal/auditor","working_directory":"internal/auditor","required":true}]}
  }, "audit_seed": {}
}`)
	if _, err := artifacts.Write(runID, "canonical_packet", "canonical_packet.json", packet); err != nil {
		t.Fatalf("write packet: %v", err)
	}

	ev := &Evidence{RunID: runID}
	(&Collector{}).collectPacketMetadata(runID, ev)
	if len(ev.Packet.ValidationCommands) != 1 {
		t.Fatalf("validation commands = %+v", ev.Packet.ValidationCommands)
	}
	command := ev.Packet.ValidationCommands[0]
	if command.ID != "V-contract" || command.WorkingDirectory != "internal/auditor" {
		t.Fatalf("canonical validation contract was not used: %+v", command)
	}
}
