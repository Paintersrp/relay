// Command publiccontractgen regenerates the aggregate compatibility contract
// from its structured JSON representation. Retired identities are removed as
// JSON members, never by editing serialized bytes.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
)

var retired = map[string]struct{}{
	"submit_plan": {}, "create_run": {}, "SubmitPlanResult": {}, "CreateRunResult": {},
}

func main() {
	dir := filepath.Join(repositoryRoot(), "internal", "operations", "registry")
	path := filepath.Join(dir, "public_contract.json")
	raw := mustRead(path)
	value := decode(raw)
	cleaned, keep := removeRetired(value)
	if !keep {
		fatalf("public contract root was retired")
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		fatalf("encode public contract: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		fatalf("write public contract: %v", err)
	}
	sum := sha256.Sum256(encoded)
	pins := fmt.Sprintf("package registry\n\nconst (\n\tPublicContractVersion = %q\n\tPublicContractBytes   = %d\n\tPublicContractSHA256  = %q\n)\n", "relay.mcp.public-contract.v1", len(encoded), hex.EncodeToString(sum[:]))
	formatted, err := format.Source([]byte(pins))
	if err != nil {
		fatalf("format pins: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "aggregate_contract_pins.go"), formatted, 0o644); err != nil {
		fatalf("write pins: %v", err)
	}
}

func decode(raw []byte) any {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		fatalf("decode public contract: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fatalf("decode public contract trailing data: %v", err)
	}
	return value
}

// removeRetired traverses JSON values without coercing json.Number. It drops
// retired descriptor map entries, enum/order elements, and definitions. A
// parent object whose sole identity is retired is also dropped.
func removeRetired(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		_, found := retired[typed]
		return typed, !found
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			cleaned, keep := removeRetired(item)
			if keep {
				out = append(out, cleaned)
			}
		}
		return out, true
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if _, found := retired[key]; found {
				continue
			}
			cleaned, keep := removeRetired(item)
			if keep {
				out[key] = cleaned
			}
		}
		if name, ok := out["name"].(string); ok {
			if _, found := retired[name]; found {
				return nil, false
			}
		}
		return out, true
	default:
		return value, true
	}
}

func repositoryRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		fatalf("cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fatalf("repository root not found")
		}
		dir = parent
	}
}
func mustRead(path string) []byte {
	raw, err := os.ReadFile(path)
	if err != nil {
		fatalf("read %s: %v", path, err)
	}
	return raw
}
func fatalf(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }
