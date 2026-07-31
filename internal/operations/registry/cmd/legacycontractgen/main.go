package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
)

func main() {
	root := findRoot()
	dir := filepath.Join(root, "internal", "operations", "registry")
	path := filepath.Join(dir, "public_contract.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	// Contract JSON remains bytes, so schema integers never traverse float64.
	raw = bytes.ReplaceAll(raw, []byte("9223372036854776000"), []byte("9223372036854775807"))
	for _, retired := range []string{"submit_plan", "create_run"} {
		raw = bytes.ReplaceAll(raw, []byte(`,"`+retired+`"`), nil)
		raw = bytes.ReplaceAll(raw, []byte(`"`+retired+`",`), nil)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		panic(err)
	}
	sum := sha256.Sum256(raw)
	content := fmt.Sprintf("package registry\n\nconst (\n\tPublicContractVersion = \"relay.mcp.public-contract.v1\"\n\tPublicContractBytes = %d\n\tPublicContractSHA256 = \"%s\"\n)\n", len(raw), hex.EncodeToString(sum[:]))
	formatted, err := format.Source([]byte(content))
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy_contract_pins.go"), formatted, 0o644); err != nil {
		panic(err)
	}
}

func findRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("repository root not found")
		}
		dir = parent
	}
}
