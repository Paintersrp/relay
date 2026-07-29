package main

import "fmt"

// validatePublishedToolCardinality checks that every published tool has one
// route-derived identity, metadata entry, and runtime binding in the same
// order. Route tools are deduplicated because shared tools legitimately occur
// on multiple routes.
func validatePublishedToolCardinality(order []string, metadata []metadataTool, bindings bindingDocument) error {
	if len(order) == 0 {
		return fmt.Errorf("published tool order is empty")
	}
	seenMetadata := make(map[string]struct{}, len(metadata))
	for _, item := range metadata {
		if _, exists := seenMetadata[item.Name]; exists {
			return fmt.Errorf("duplicate metadata %q", item.Name)
		}
		seenMetadata[item.Name] = struct{}{}
	}

	seenBindingOrder := make(map[string]struct{}, len(bindings.BindingOrder))
	for _, name := range bindings.BindingOrder {
		if _, exists := seenBindingOrder[name]; exists {
			return fmt.Errorf("duplicate binding order %q", name)
		}
		seenBindingOrder[name] = struct{}{}
	}

	if len(bindings.BindingOrder) != len(order) {
		return fmt.Errorf("binding order cardinality differs")
	}
	for index, name := range order {
		if bindings.BindingOrder[index] != name {
			return fmt.Errorf("binding order %q differs at index %d", bindings.BindingOrder[index], index)
		}
	}

	routeNames := make(map[string]struct{}, len(order))
	for _, name := range order {
		routeNames[name] = struct{}{}
	}
	for _, name := range order {
		if _, exists := seenMetadata[name]; !exists {
			return fmt.Errorf("metadata %q missing", name)
		}
		if _, exists := bindings.Bindings[name]; !exists {
			return fmt.Errorf("binding %q missing", name)
		}
	}
	for name := range seenMetadata {
		if _, exists := routeNames[name]; !exists {
			return fmt.Errorf("metadata %q is extra", name)
		}
	}
	for name := range bindings.Bindings {
		if _, exists := routeNames[name]; !exists {
			return fmt.Errorf("binding %q is extra", name)
		}
	}
	return nil
}
