package speccompiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

type nodeKind uint8

const (
	nodeNull nodeKind = iota
	nodeObject
	nodeArray
	nodeString
	nodeNumber
	nodeBool
)

type objectEntry struct {
	key   string
	value *jsonNode
}

type jsonNode struct {
	kind    nodeKind
	object  []objectEntry
	array   []*jsonNode
	text    string
	number  json.Number
	boolean bool
}

func parseDocument(raw []byte) (*jsonNode, []Diagnostic) {
	if !utf8.Valid(raw) {
		return nil, []Diagnostic{
			{Code: "invalid_utf8", Path: "", Message: "Artifact is not valid UTF-8."}}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	root, diagnostics, err := parseValue(dec, "")
	if err != nil {
		return nil, []Diagnostic{
			{Code: "invalid_json", Path: "", Message: err.Error()}}
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, []Diagnostic{
			{Code: "invalid_json", Path: "", Message: err.Error()}}
	}
	if len(diagnostics) != 0 {
		return nil, diagnostics
	}
	return root, nil
}

func parseValue(dec *json.Decoder, path string) (*jsonNode, []Diagnostic, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	switch value := tok.(type) {
	case nil:
		return &jsonNode{kind: nodeNull}, nil, nil
	case string:
		return &jsonNode{kind: nodeString, text: value}, nil, nil
	case json.Number:
		return &jsonNode{kind: nodeNumber, number: value}, nil, nil
	case float64:
		return &jsonNode{kind: nodeNumber, number: json.Number(strconv.FormatFloat(value, 'g', -1, 64))}, nil, nil
	case bool:
		return &jsonNode{kind: nodeBool, boolean: value}, nil, nil
	case json.Delim:
		switch value {
		case '{':
			return parseObject(dec, path)
		case '[':
			return parseArray(dec, path)
		default:
			return nil, nil, fmt.Errorf("unexpected delimiter %q", value)
		}
	default:
		return nil, nil, fmt.Errorf("unexpected JSON token %T", tok)
	}
}

func parseObject(dec *json.Decoder, path string) (*jsonNode, []Diagnostic, error) {
	node := &jsonNode{kind: nodeObject}
	seen := map[string]struct{}{}
	var diagnostics []Diagnostic
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, diagnostics, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, diagnostics, fmt.Errorf("object key is not a string")
		}
		childPath := joinPointer(path, key)
		child, childDiagnostics, err := parseValue(dec, childPath)
		if err != nil {
			return nil, append(diagnostics, childDiagnostics...), err
		}
		diagnostics = append(diagnostics, childDiagnostics...)
		if _, duplicate := seen[key]; duplicate {
			diagnostics = append(diagnostics, Diagnostic{Code: "duplicate_object_key", Path: childPath, Message: fmt.Sprintf("Object key %q appears more than once.", key)})
		} else {
			seen[key] = struct{}{}
		}
		node.object = append(node.object, objectEntry{key: key, value: child})
	}
	end, err := dec.Token()
	if err != nil {
		return nil, diagnostics, err
	}
	if end != json.Delim('}') {
		return nil, diagnostics, fmt.Errorf("object did not end with }")
	}
	return node, diagnostics, nil
}

func parseArray(dec *json.Decoder, path string) (*jsonNode, []Diagnostic, error) {
	node := &jsonNode{kind: nodeArray}
	var diagnostics []Diagnostic
	for i := 0; dec.More(); i++ {
		child, childDiagnostics, err := parseValue(dec, joinPointer(path, strconv.Itoa(i)))
		if err != nil {
			return nil, append(diagnostics, childDiagnostics...), err
		}
		diagnostics = append(diagnostics, childDiagnostics...)
		node.array = append(node.array, child)
	}
	end, err := dec.Token()
	if err != nil {
		return nil, diagnostics, err
	}
	if end != json.Delim(']') {
		return nil, diagnostics, fmt.Errorf("array did not end with ]")
	}
	return node, diagnostics, nil
}

func (n *jsonNode) objectMember(key string) (objectEntry, bool) {
	if n == nil || n.kind != nodeObject {
		return objectEntry{}, false
	}
	for _, entry := range n.object {
		if entry.key == key {
			return entry, true
		}
	}
	return objectEntry{}, false
}

func (n *jsonNode) describe() string {
	if n == nil {
		return "absent"
	}
	switch n.kind {
	case nodeNull:
		return "null"
	case nodeObject:
		return "an object"
	case nodeArray:
		return "an array"
	case nodeString:
		return strconv.Quote(n.text)
	case nodeNumber:
		return n.number.String()
	case nodeBool:
		return strconv.FormatBool(n.boolean)
	default:
		return "an unknown value"
	}
}

func joinPointer(base, token string) string {
	escaped := strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
	return base + "/" + escaped
}
