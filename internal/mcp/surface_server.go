package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"relay/internal/mcp/surfacecontracts"
	"relay/internal/operations/registry"
)

type SurfaceHandler func(json.RawMessage) ToolCallResult

type surfaceDispatch struct {
	surface registry.SurfaceContractID
	handle  SurfaceHandler
}

type SurfaceToolHandler struct {
	Name     string
	ReadOnly bool
	Handle   SurfaceHandler
}

func ValidateCompiledSurfaceCatalog() error {
	return surfacecontracts.Validate()
}

func NewServerForSurface(log *slog.Logger, deps *MCPDeps, surface registry.SurfaceContractID, handlers []SurfaceToolHandler) (*Server, error) {
	if err := ValidateCompiledSurfaceCatalog(); err != nil {
		return nil, err
	}
	manifest, ok := surfacecontracts.Get(surface)
	if !ok {
		return nil, fmt.Errorf("unknown surface contract %q", surface)
	}
	if len(handlers) != len(manifest.Tools) {
		return nil, fmt.Errorf("surface %q requires %d handlers, got %d", surface, len(manifest.Tools), len(handlers))
	}

	definitions := make([]ToolDefinition, len(manifest.Tools))
	dispatch := make(map[string]surfaceDispatch, len(manifest.Tools))
	for index, tool := range manifest.Tools {
		handler := handlers[index]
		if handler.Name != tool.Name {
			return nil, fmt.Errorf("surface %q handler %d is %q, want %q", surface, index, handler.Name, tool.Name)
		}
		if handler.Handle == nil {
			return nil, fmt.Errorf("surface %q handler %q is nil", surface, handler.Name)
		}
		if _, duplicate := dispatch[handler.Name]; duplicate {
			return nil, fmt.Errorf("surface %q handler %q is duplicated", surface, handler.Name)
		}
		if handler.ReadOnly != tool.Annotations.ReadOnlyHint {
			return nil, fmt.Errorf("surface %q handler %q read-only classification differs from manifest", surface, handler.Name)
		}
		dispatch[handler.Name] = surfaceDispatch{surface: surface, handle: handler.Handle}
		definitions[index] = toolDefinitionFromContract(tool)
	}
	for name := range dispatch {
		found := false
		for _, tool := range manifest.Tools {
			if tool.Name == name {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("surface %q has extra handler %q", surface, name)
		}
	}

	return &Server{
		log:             log,
		deps:            deps,
		tools:           definitions,
		surfaceHandlers: dispatch,
	}, nil
}

func toolDefinitionFromContract(tool surfacecontracts.ToolContract) ToolDefinition {
	annotations := map[string]any{
		"readOnlyHint":    tool.Annotations.ReadOnlyHint,
		"destructiveHint": tool.Annotations.DestructiveHint,
		"idempotentHint":  tool.Annotations.IdempotentHint,
		"openWorldHint":   tool.Annotations.OpenWorldHint,
	}
	metadata := map[string]any{
		"openai/widgetAccessible":        false,
		"openai/toolInvocation/invoking": tool.Invoking,
		"openai/toolInvocation/invoked":  tool.Invoked,
		"relay/surfaceContract":          string(tool.SurfaceContract),
		"relay/surfaceManifestSHA256":    tool.ManifestSHA256,
		"relay/semanticToolID":           tool.SemanticToolID,
	}
	if len(tool.FileParams) != 0 {
		metadata["openai/fileParams"] = append([]string(nil), tool.FileParams...)
	}
	return ToolDefinition{
		Name:               tool.Name,
		Title:              tool.Title,
		Description:        tool.Description,
		InputSchema:        append(json.RawMessage(nil), tool.InputSchema...),
		OutputSchema:       append(json.RawMessage(nil), tool.OutputSchema...),
		Annotations:        annotations,
		Meta:               metadata,
		orderedAnnotations: orderedAnnotations(tool),
		orderedMeta:        orderedMetadata(tool),
	}
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

func orderedAnnotations(tool surfacecontracts.ToolContract) json.RawMessage {
	var output bytes.Buffer
	output.WriteByte('{')
	writeMCPNameBool(&output, "readOnlyHint", tool.Annotations.ReadOnlyHint, false)
	writeMCPNameBool(&output, "destructiveHint", tool.Annotations.DestructiveHint, true)
	writeMCPNameBool(&output, "idempotentHint", tool.Annotations.IdempotentHint, true)
	writeMCPNameBool(&output, "openWorldHint", tool.Annotations.OpenWorldHint, true)
	output.WriteByte('}')
	return output.Bytes()
}

func orderedMetadata(tool surfacecontracts.ToolContract) json.RawMessage {
	var output bytes.Buffer
	output.WriteByte('{')
	writeMCPNameBool(&output, "openai/widgetAccessible", false, false)
	writeMCPNameString(&output, "openai/toolInvocation/invoking", tool.Invoking, true)
	writeMCPNameString(&output, "openai/toolInvocation/invoked", tool.Invoked, true)
	if len(tool.FileParams) != 0 {
		output.WriteString(`,"openai/fileParams":[`)
		for index, fileParam := range tool.FileParams {
			if index > 0 {
				output.WriteByte(',')
			}
			appendMCPJSONString(&output, fileParam)
		}
		output.WriteByte(']')
	}
	writeMCPNameString(&output, "relay/surfaceContract", string(tool.SurfaceContract), true)
	writeMCPNameString(&output, "relay/surfaceManifestSHA256", tool.ManifestSHA256, true)
	writeMCPNameString(&output, "relay/semanticToolID", tool.SemanticToolID, true)
	output.WriteByte('}')
	return output.Bytes()
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
	if _, err := registry.ValidateRequest(dispatch.surface, name, args); err != nil {
		return ToolCallResult{}, err
	}
	return dispatch.handle(args), nil
}
