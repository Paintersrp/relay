package main

import "testing"

func testCardinalityFixture() ([]string, []metadataTool, bindingDocument) {
	order := []string{"shared", "second"}
	metadata := []metadataTool{{Name: "shared"}, {Name: "second"}}
	bindings := bindingDocument{
		BindingOrder: []string{"shared", "second"},
		Bindings: map[string]binding{
			"shared": {ToolName: "shared"},
			"second": {ToolName: "second"},
		},
	}
	return order, metadata, bindings
}

func TestValidatePublishedToolCardinalityValidDynamicallySizedContract(t *testing.T) {
	order, metadata, bindings := testCardinalityFixture()
	if err := validatePublishedToolCardinality(order, metadata, bindings); err != nil {
		t.Fatal(err)
	}
}

func TestValidatePublishedToolCardinalityRejectsDuplicateMetadataName(t *testing.T) {
	order, metadata, bindings := testCardinalityFixture()
	metadata = append(metadata, metadata[0])
	if err := validatePublishedToolCardinality(order, metadata, bindings); err == nil {
		t.Fatal("expected duplicate metadata error")
	}
}

func TestValidatePublishedToolCardinalityRejectsDuplicateBindingOrderName(t *testing.T) {
	order, metadata, bindings := testCardinalityFixture()
	bindings.BindingOrder = []string{"shared", "shared"}
	if err := validatePublishedToolCardinality(order, metadata, bindings); err == nil {
		t.Fatal("expected duplicate binding-order error")
	}
}

func TestValidatePublishedToolCardinalityRejectsMissingMetadata(t *testing.T) {
	order, metadata, bindings := testCardinalityFixture()
	metadata = metadata[:1]
	if err := validatePublishedToolCardinality(order, metadata, bindings); err == nil {
		t.Fatal("expected missing metadata error")
	}
}

func TestValidatePublishedToolCardinalityRejectsExtraMetadata(t *testing.T) {
	order, metadata, bindings := testCardinalityFixture()
	metadata = append(metadata, metadataTool{Name: "extra"})
	if err := validatePublishedToolCardinality(order, metadata, bindings); err == nil {
		t.Fatal("expected extra metadata error")
	}
}

func TestValidatePublishedToolCardinalityRejectsMissingBinding(t *testing.T) {
	order, metadata, bindings := testCardinalityFixture()
	delete(bindings.Bindings, "second")
	if err := validatePublishedToolCardinality(order, metadata, bindings); err == nil {
		t.Fatal("expected missing binding error")
	}
}

func TestValidatePublishedToolCardinalityRejectsExtraBinding(t *testing.T) {
	order, metadata, bindings := testCardinalityFixture()
	bindings.Bindings["extra"] = binding{ToolName: "extra"}
	if err := validatePublishedToolCardinality(order, metadata, bindings); err == nil {
		t.Fatal("expected extra binding error")
	}
}

func TestValidatePublishedToolCardinalityRejectsBindingOrderMismatch(t *testing.T) {
	order, metadata, bindings := testCardinalityFixture()
	bindings.BindingOrder = []string{"second", "shared"}
	if err := validatePublishedToolCardinality(order, metadata, bindings); err == nil {
		t.Fatal("expected binding-order mismatch error")
	}
}
