package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestRegenerationPreservesMaximumJSONIntegerAndSchemaDigest(t *testing.T) {
	const schema = `{"maximum":9223372036854775807,"type":"integer"}`
	value := decode([]byte(schema))
	regenerated, keep := removeRetired(value)
	if !keep {
		t.Fatal("maximum-integer schema was removed")
	}
	raw, err := json.Marshal(regenerated)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, []byte(schema)) {
		t.Fatalf("regenerated schema = %s, want %s", raw, schema)
	}
	if bytes.Contains(raw, []byte("9223372036854776000")) {
		t.Fatalf("maximum integer was rounded: %s", raw)
	}
	sum := sha256.Sum256(raw)
	expected := sha256.Sum256([]byte(schema))
	if got := hex.EncodeToString(sum[:]); got != hex.EncodeToString(expected[:]) {
		t.Fatalf("schema sha256 = %s", got)
	}
}
