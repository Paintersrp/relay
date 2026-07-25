package registry

import (
	"encoding/json"

	sourcecontract "relay/contracts/source"
)

const (
	SourceToolListTree = sourcecontract.ToolListTree
	SourceToolSearch   = sourcecontract.ToolSearch
	SourceToolReadText = sourcecontract.ToolReadText
)

func OwnedSourceToolContract(tool string) bool { return sourcecontract.Owns(tool) }

func SourceToolContractSchemas(tool string) (json.RawMessage, json.RawMessage, bool) {
	return sourcecontract.Schemas(tool)
}
