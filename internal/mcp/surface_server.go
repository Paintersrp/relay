package mcp

import (
	"bytes"
	"encoding/json"
	"errors"

	"relay/internal/operations/registry"
)

type SurfaceHandler func(json.RawMessage) ToolCallResult

type surfaceDispatch struct {
	surface     registry.SurfaceContractID
	toolName    string
	routeBound  bool
	staticRoute bool
	handle      SurfaceHandler
}

func (tool ToolDefinition) MarshalJSON() ([]byte, error) {
	var output bytes.Buffer
	output.WriteByte('{')
	writeMCPNameString(&output, "name", tool.Name, false)
	if tool.Title != "" {
		writeMCPNameString(&output, "title", tool.Title, true)
	}
	writeMCPNameString(&output, "description", tool.Description, true)
	output.WriteString(`,"inputSchema":`)
	if len(tool.InputSchema) == 0 {
		output.WriteString("null")
	} else {
		output.Write(tool.InputSchema)
	}
	if len(tool.OutputSchema) != 0 {
		output.WriteString(`,"outputSchema":`)
		output.Write(tool.OutputSchema)
	}
	if len(tool.orderedAnnotations) != 0 {
		output.WriteString(`,"annotations":`)
		output.Write(tool.orderedAnnotations)
	} else if len(tool.Annotations) != 0 {
		encoded, err := json.Marshal(tool.Annotations)
		if err != nil {
			return nil, err
		}
		output.WriteString(`,"annotations":`)
		output.Write(encoded)
	}
	if len(tool.orderedMeta) != 0 {
		output.WriteString(`,"_meta":`)
		output.Write(tool.orderedMeta)
	} else if len(tool.Meta) != 0 {
		encoded, err := json.Marshal(tool.Meta)
		if err != nil {
			return nil, err
		}
		output.WriteString(`,"_meta":`)
		output.Write(encoded)
	}
	output.WriteByte('}')
	return output.Bytes(), nil
}

func writeMCPNameString(output *bytes.Buffer, name, value string, comma bool) {
	if comma {
		output.WriteByte(',')
	}
	appendMCPJSONString(output, name)
	output.WriteByte(':')
	appendMCPJSONString(output, value)
}

func writeMCPNameBool(output *bytes.Buffer, name string, value, comma bool) {
	if comma {
		output.WriteByte(',')
	}
	appendMCPJSONString(output, name)
	output.WriteByte(':')
	if value {
		output.WriteString("true")
	} else {
		output.WriteString("false")
	}
}

func appendMCPJSONString(output *bytes.Buffer, value string) {
	encoded, _ := json.Marshal(value)
	output.Write(encoded)
}

func (s *Server) dispatchSurfaceTool(name string, args json.RawMessage) (ToolCallResult, error) {
	if s == nil || s.surfaceHandlers == nil {
		return ToolCallResult{}, errors.New("surface dispatcher is not configured")
	}
	dispatch, ok := s.surfaceHandlers[name]
	if !ok {
		return ToolCallResult{}, errors.New("surface handler is not configured")
	}
	if dispatch.staticRoute {
		boundArgs, err := withDefaultSurfaceContract(args, dispatch.surface)
		if dispatch.routeBound {
			boundArgs, err = withBoundSurfaceContract(args, dispatch.surface)
		}
		if err != nil {
			return ToolCallResult{}, err
		}
		if err := registry.ValidateOperationRequest(dispatch.surface, dispatch.toolName, boundArgs); err != nil {
			return ToolCallResult{}, err
		}
		if !dispatch.routeBound {
			return dispatch.handle(args), nil
		}
		return dispatch.handle(boundArgs), nil
	}
	if err := registry.ValidateOperationRequest(dispatch.surface, dispatch.toolName, args); err != nil {
		return ToolCallResult{}, err
	}
	return dispatch.handle(args), nil
}

func withBoundSurfaceContract(raw json.RawMessage, surface registry.SurfaceContractID) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.Marshal(map[string]any{"surface_contract": surface})
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return raw, nil
	}
	encodedSurface, err := json.Marshal(surface)
	if err != nil {
		return nil, err
	}
	object["surface_contract"] = encodedSurface
	return json.Marshal(object)
}

func withDefaultSurfaceContract(raw json.RawMessage, surface registry.SurfaceContractID) (json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return json.Marshal(map[string]any{"surface_contract": surface})
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return raw, nil
	}
	if _, exists := object["surface_contract"]; exists {
		return raw, nil
	}
	encodedSurface, err := json.Marshal(surface)
	if err != nil {
		return nil, err
	}
	object["surface_contract"] = encodedSurface
	return json.Marshal(object)
}
